package game

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type GameService interface {
	MakeMove(ctx context.Context, gameID string, playerID int64, move string, cachedState ...interface{}) (*Game, error)
	ResignGame(ctx context.Context, gameID string, playerID int64) (*Game, error)
	DrawGame(ctx context.Context, gameID string, playerID int64) (*Game, error)
	OfferDraw(ctx context.Context, gameID string, playerID int64) (*Game, bool, int64, error)
	AcceptDraw(ctx context.Context, gameID string, playerID int64) (*Game, error)
	DeclineDraw(ctx context.Context, gameID string, playerID int64) (*Game, error)
	GetGame(ctx context.Context, gameID string) (*Game, error)
	GetMoves(ctx context.Context, gameID string) ([]GameMove, error)
	GetPlayerGames(ctx context.Context, playerID int64) ([]Game, error)
	SetGameCompletionListener(listener func(ctx context.Context, gameID string, result string))
	SetGameStateUpdatedListener(listener func(ctx context.Context, g *Game))
	CheckGameTimeout(ctx context.Context, currentGame *Game) (bool, error)
	CheckAllTimeouts(ctx context.Context) ([]*Game, error)
	CheckLiveTimeouts(ctx context.Context) ([]*Game, error)
	ClaimTimeout(ctx context.Context, gameID string) (*Game, error)
	StartTimeoutChecker(ctx context.Context, interval time.Duration)
	EndGameOnDisconnect(ctx context.Context, gameID string, disconnectedPlayerID int64) (*Game, error)
}

const moveWorkerCount = 16

const firstMoveTimeoutMs = 60000

func ClockStarted(g *Game) bool {
	parts := strings.Fields(g.Position)
	if len(parts) < 6 {
		return true
	}
	return parts[5] != "1"
}

type movePersistTask struct {
	GameID        string     `json:"game_id"`
	Position      string     `json:"position"`
	Status        string     `json:"status"`
	Result        string     `json:"result"`
	EndReason     string     `json:"end_reason"`
	NextTurn      string     `json:"next_turn"`
	WhiteTimeMs   int64      `json:"white_time_ms"`
	BlackTimeMs   int64      `json:"black_time_ms"`
	TurnStartedAt *time.Time `json:"turn_started_at"`
	Move          *GameMove  `json:"move"`
	WhitePlayerID int64      `json:"white_player_id"`
	BlackPlayerID int64      `json:"black_player_id"`
	Game          *Game      `json:"game"`
	ExpectedTurn  string     `json:"expected_turn"`
}

type Service struct {
	Repository         GameRepository
	RedisClient        *redis.Client
	OnGameCompleted    func(ctx context.Context, gameID string, result string)
	OnGameStateUpdated func(ctx context.Context, g *Game)
	drawOffers         sync.Map
	gameLocks          sync.Map
	moveQueues         [moveWorkerCount]chan movePersistTask
	pendingMu          sync.Mutex
	pendingMoves       map[string]int
}

func (s *Service) getGameLock(gameID string) *sync.Mutex {
	val, _ := s.gameLocks.LoadOrStore(gameID, &sync.Mutex{})
	return val.(*sync.Mutex)
}

func (s *Service) addPendingMove(gameID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.pendingMoves[gameID]++
}

func (s *Service) removePendingMove(gameID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if s.pendingMoves[gameID] <= 1 {
		delete(s.pendingMoves, gameID)
		return
	}
	s.pendingMoves[gameID]--
}

func (s *Service) hasPendingMoves(gameID string) bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	return s.pendingMoves[gameID] > 0
}

func (s *Service) waitForPendingMoves(gameID string) {
	for i := 0; i < 100; i++ {
		if !s.hasPendingMoves(gameID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	slog.Warn("pending moves not saved in time", "game_id", gameID)
}

func NewService(repository GameRepository, redisClient ...*redis.Client) GameService {
	s := &Service{
		Repository:   repository,
		pendingMoves: make(map[string]int),
	}
	if len(redisClient) > 0 {
		s.RedisClient = redisClient[0]
	}
	for i := 0; i < moveWorkerCount; i++ {
		s.moveQueues[i] = make(chan movePersistTask, 256)
		go s.startMoveWorker(i, s.moveQueues[i])
	}
	return s
}

func (s *Service) getWorkerIndex(gameID string) int {
	var h uint32 = 2166136261
	for i := 0; i < len(gameID); i++ {
		h ^= uint32(gameID[i])
		h *= 16777619
	}
	return int(h % moveWorkerCount)
}

func (s *Service) enqueueMovePersist(task movePersistTask) {
	idx := s.getWorkerIndex(task.GameID)
	if s.RedisClient != nil {
		data, err := json.Marshal(task)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err = s.RedisClient.RPush(ctx, fmt.Sprintf("caisaara:moves:queue:%d", idx), data).Err()
			cancel()
			if err == nil {
				return
			}
			slog.ErrorContext(ctx, "failed to push move to redis queue", "game_id", task.GameID, "error", err)
		}
	}
	if s.moveQueues[idx] == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.Repository.SaveMoveTx(
			ctx,
			task.GameID,
			task.Position,
			task.Status,
			task.Result,
			task.EndReason,
			task.NextTurn,
			task.WhiteTimeMs,
			task.BlackTimeMs,
			task.TurnStartedAt,
			task.Move,
			task.ExpectedTurn,
		)
		cancel()
		s.removePendingMove(task.GameID)
		return
	}
	s.moveQueues[idx] <- task
}

func (s *Service) processMovePersist(task movePersistTask) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.Repository.SaveMoveTx(
			ctx,
			task.GameID,
			task.Position,
			task.Status,
			task.Result,
			task.EndReason,
			task.NextTurn,
			task.WhiteTimeMs,
			task.BlackTimeMs,
			task.TurnStartedAt,
			task.Move,
			task.ExpectedTurn,
		)
		cancel()
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	s.removePendingMove(task.GameID)
	if lastErr != nil {
		slog.Error("save move transaction failed in worker after retries", "game_id", task.GameID, "error", lastErr)
		return
	}
	if task.Status == StatusFinished {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.notifyGameCompleted(ctx, task.Game)
		s.notifyGameStateUpdated(ctx, task.Game)
		cancel()
	}
}

func (s *Service) startMoveWorker(workerIdx int, queue <-chan movePersistTask) {
	queueKey := fmt.Sprintf("caisaara:moves:queue:%d", workerIdx)
	for {
		if s.RedisClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			val, err := s.RedisClient.BLPop(ctx, 1*time.Second, queueKey).Result()
			cancel()
			if err == nil && len(val) >= 2 {
				var task movePersistTask
				if err := json.Unmarshal([]byte(val[1]), &task); err == nil {
					s.processMovePersist(task)
					continue
				}
			}
		}

		select {
		case task := <-queue:
			s.processMovePersist(task)
		default:
			if s.RedisClient == nil {
				task, ok := <-queue
				if !ok {
					return
				}
				s.processMovePersist(task)
			} else {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}

func (s *Service) SetGameCompletionListener(listener func(ctx context.Context, gameID string, result string)) {
	s.OnGameCompleted = listener
}

func (s *Service) notifyGameCompleted(ctx context.Context, g *Game) {
	if s.OnGameCompleted != nil {
		s.OnGameCompleted(ctx, g.ID, g.Result)
	}
	if updated, err := s.Repository.FindGameByID(ctx, g.ID); err == nil && updated != nil {
		g.WhiteRatingBefore = updated.WhiteRatingBefore
		g.WhiteRatingAfter = updated.WhiteRatingAfter
		g.BlackRatingBefore = updated.BlackRatingBefore
		g.BlackRatingAfter = updated.BlackRatingAfter
		g.WhiteProvisional = updated.WhiteProvisional
		g.BlackProvisional = updated.BlackProvisional
	}
}

func (s *Service) SetGameStateUpdatedListener(listener func(ctx context.Context, g *Game)) {
	s.OnGameStateUpdated = listener
}

func (s *Service) notifyGameStateUpdated(ctx context.Context, g *Game) {
	if s.OnGameStateUpdated != nil {
		s.OnGameStateUpdated(ctx, g)
	}
}

func (s *Service) CheckGameTimeout(ctx context.Context, currentGame *Game) (bool, error) {
	if currentGame == nil || currentGame.Status != StatusActive {
		return false, nil
	}

	now := time.Now()
	var turnStartedAt time.Time
	switch {
	case currentGame.TurnStartedAt != nil:
		turnStartedAt = *currentGame.TurnStartedAt
	case currentGame.StartedAt != nil:
		turnStartedAt = *currentGame.StartedAt
	default:
		turnStartedAt = now
	}

	elapsedMs := now.Sub(turnStartedAt).Milliseconds()
	if elapsedMs < 0 {
		elapsedMs = 0
	}

	isWhiteTurn := currentGame.CurrentTurn == "w" || currentGame.CurrentTurn == "White"
	var timedOut bool
	var result, endReason string
	newWhiteTimeMs := currentGame.WhiteTimeMs
	newBlackTimeMs := currentGame.BlackTimeMs

	switch {
	case currentGame.TimeControlMode == ModeDaily:
		dailyLimitMs := currentGame.DailyMoveTimeMs
		if dailyLimitMs <= 0 {
			dailyLimitMs = 86400000
		}
		if elapsedMs > dailyLimitMs {
			timedOut = true
			endReason = EndReasonDailyTimeout
			if isWhiteTurn {
				result = ResultBlackWin
				newWhiteTimeMs = 0
			} else {
				result = ResultWhiteWin
				newBlackTimeMs = 0
			}
		}
	case !ClockStarted(currentGame):
		if elapsedMs >= firstMoveTimeoutMs {
			timedOut = true
			endReason = EndReasonAbandonment
			if isWhiteTurn {
				result = ResultBlackWin
			} else {
				result = ResultWhiteWin
			}
		}
	default:
		if isWhiteTurn {
			newWhiteTimeMs = currentGame.WhiteTimeMs - elapsedMs
			if newWhiteTimeMs <= 0 {
				newWhiteTimeMs = 0
				timedOut = true
				endReason = EndReasonTimeout
				result = ResultBlackWin
			}
		} else {
			newBlackTimeMs = currentGame.BlackTimeMs - elapsedMs
			if newBlackTimeMs <= 0 {
				newBlackTimeMs = 0
				timedOut = true
				endReason = EndReasonTimeout
				result = ResultWhiteWin
			}
		}
	}

	if !timedOut {
		return false, nil
	}

	s.waitForPendingMoves(currentGame.ID)

	err := s.Repository.UpdateGameState(
		ctx,
		currentGame.ID,
		currentGame.Position,
		StatusFinished,
		result,
		endReason,
		currentGame.CurrentTurn,
		newWhiteTimeMs,
		newBlackTimeMs,
		&now,
	)
	if err != nil {
		slog.ErrorContext(ctx, "update game state on timeout failed", "game_id", currentGame.ID, "error", err)
		return false, err
	}

	currentGame.Status = StatusFinished
	currentGame.Result = result
	currentGame.EndReason = endReason
	currentGame.WhiteTimeMs = newWhiteTimeMs
	currentGame.BlackTimeMs = newBlackTimeMs
	currentGame.TurnStartedAt = &now

	s.notifyGameCompleted(ctx, currentGame)
	s.notifyGameStateUpdated(ctx, currentGame)

	slog.InfoContext(ctx, "game ended by timeout", "game_id", currentGame.ID, "result", result, "end_reason", endReason)
	return true, nil
}

func (s *Service) CheckAllTimeouts(ctx context.Context) ([]*Game, error) {
	return s.checkTimeouts(ctx, true)
}

func (s *Service) CheckLiveTimeouts(ctx context.Context) ([]*Game, error) {
	return s.checkTimeouts(ctx, false)
}

func (s *Service) checkTimeouts(ctx context.Context, includeDaily bool) ([]*Game, error) {
	activeGames, err := s.Repository.GetTimedOutGames(ctx, includeDaily, 100)
	if err != nil {
		return nil, err
	}

	var timedOutGames []*Game
	for i := range activeGames {
		gameID := activeGames[i].ID
		mu := s.getGameLock(gameID)
		mu.Lock()
		s.waitForPendingMoves(gameID)
		latestGame, err := s.Repository.FindGameByID(ctx, gameID)
		if err != nil {
			mu.Unlock()
			slog.ErrorContext(ctx, "find game for timeout check failed", "game_id", gameID, "error", err)
			continue
		}
		timedOut, err := s.CheckGameTimeout(ctx, latestGame)
		mu.Unlock()
		if err != nil {
			slog.ErrorContext(ctx, "check active game timeout failed", "game_id", gameID, "error", err)
			continue
		}
		if timedOut {
			timedOutGames = append(timedOutGames, latestGame)
		}
	}

	return timedOutGames, nil
}

func (s *Service) ClaimTimeout(ctx context.Context, gameID string) (*Game, error) {
	mu := s.getGameLock(gameID)
	mu.Lock()
	defer mu.Unlock()

	s.waitForPendingMoves(gameID)

	currentGame, err := s.Repository.FindGameByID(ctx, gameID)
	if err != nil {
		return nil, err
	}
	if currentGame.Status != StatusActive {
		return currentGame, nil
	}
	timedOut, err := s.CheckGameTimeout(ctx, currentGame)
	if err != nil {
		return nil, err
	}
	if !timedOut {
		return currentGame, errors.New("time has not expired")
	}
	return currentGame, nil
}

func (s *Service) StartTimeoutChecker(ctx context.Context, interval time.Duration) {
	go func() {
		liveTicker := time.NewTicker(interval)
		defer liveTicker.Stop()

		dailyTicker := time.NewTicker(1 * time.Minute)
		defer dailyTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-liveTicker.C:
				_, _ = s.checkTimeouts(ctx, false)
			case <-dailyTicker.C:
				_, _ = s.checkTimeouts(ctx, true)
			}
		}
	}()
}

func (s *Service) MakeMove(
	ctx context.Context,
	gameID string,
	playerID int64,
	move string,
	cachedState ...interface{},
) (*Game, error) {
	mu := s.getGameLock(gameID)
	mu.Lock()
	defer mu.Unlock()

	s.drawOffers.Delete(gameID)

	var currentGame *Game
	var existingMoves []GameMove
	for _, arg := range cachedState {
		switch v := arg.(type) {
		case *Game:
			currentGame = v
		case []GameMove:
			existingMoves = v
		}
	}

	if currentGame == nil {
		var err error
		currentGame, err = s.Repository.FindGameByID(ctx, gameID)
		if err != nil {
			slog.ErrorContext(ctx, "find game failed during make move", "game_id", gameID, "error", err)
			return nil, err
		}
	}

	if currentGame.Status != StatusActive {
		slog.WarnContext(ctx, "game is not active", "game_id", gameID, "status", currentGame.Status)
		return nil, errors.New("game is already finished")
	}

	if playerID != currentGame.WhitePlayerID && playerID != currentGame.BlackPlayerID {
		slog.WarnContext(ctx, "player not in game", "game_id", gameID, "player_id", playerID)
		return nil, errors.New("player is not part of this game")
	}

	if existingMoves == nil {
		var err error
		existingMoves, err = s.Repository.GetMoves(ctx, gameID)
		if err != nil {
			slog.ErrorContext(ctx, "fetch moves count failed", "game_id", gameID, "error", err)
			return nil, err
		}
	}

	chessGame, err := NewChessGameFromMoves(existingMoves)
	if err != nil {
		chessGame, err = NewChessGameFromFEN(currentGame.Position)
		if err != nil {
			slog.ErrorContext(ctx, "parse FEN position failed", "game_id", gameID, "position", currentGame.Position, "error", err)
			return nil, err
		}
	}

	if !s.isPlayerTurn(chessGame, currentGame, playerID) {
		slog.WarnContext(ctx, "not player turn", "game_id", gameID, "player_id", playerID, "turn", chessGame.Turn())
		return nil, errors.New("not your turn")
	}

	now := time.Now()
	var turnStartedAt time.Time
	switch {
	case currentGame.TurnStartedAt != nil:
		turnStartedAt = *currentGame.TurnStartedAt
	case currentGame.StartedAt != nil:
		turnStartedAt = *currentGame.StartedAt
	default:
		turnStartedAt = now
	}

	elapsedMs := now.Sub(turnStartedAt).Milliseconds()
	if elapsedMs < 0 {
		elapsedMs = 0
	}
	if !ClockStarted(currentGame) {
		elapsedMs = 0
	}

	isWhiteTurn := currentGame.CurrentTurn == "w" || currentGame.CurrentTurn == "White"
	status := StatusActive
	result := ""
	endReason := ""
	newWhiteTimeMs := currentGame.WhiteTimeMs
	newBlackTimeMs := currentGame.BlackTimeMs

	timedOut, err := s.CheckGameTimeout(ctx, currentGame)
	if err != nil {
		return nil, err
	}
	if timedOut {
		return currentGame, errors.New("time has expired")
	}

	if currentGame.TimeControlMode == ModeDaily {
		dailyLimitMs := currentGame.DailyMoveTimeMs
		if dailyLimitMs <= 0 {
			dailyLimitMs = 86400000
		}
		newWhiteTimeMs = dailyLimitMs
		newBlackTimeMs = dailyLimitMs
	} else {
		if isWhiteTurn {
			newWhiteTimeMs = currentGame.WhiteTimeMs - elapsedMs
		} else {
			newBlackTimeMs = currentGame.BlackTimeMs - elapsedMs
		}
	}

	expectedTurn := chessGame.Turn()

	if err := chessGame.MakeMove(move); err != nil {
		slog.WarnContext(ctx, "invalid move execution", "game_id", gameID, "player_id", playerID, "move", move, "error", err)
		return nil, errors.New("invalid move")
	}

	if currentGame.TimeControlMode != ModeDaily {
		if isWhiteTurn {
			newWhiteTimeMs += currentGame.IncrementMs
		} else {
			newBlackTimeMs += currentGame.IncrementMs
		}
	}

	moveNumber := len(existingMoves) + 1

	position := chessGame.FEN()

	if chessGame.IsFinished() {
		status = StatusFinished
		result, endReason = getGameResultAndEndReason(chessGame)
	}

	nextTurn := chessGame.Turn()

	gameMove := NewGameMove(
		gameID,
		playerID,
		moveNumber,
		move,
		position,
	)
	gameMove.CreatedAt = now

	err = s.Repository.SaveMoveTx(
		ctx,
		gameID,
		position,
		status,
		result,
		endReason,
		nextTurn,
		newWhiteTimeMs,
		newBlackTimeMs,
		&now,
		gameMove,
		expectedTurn,
	)
	if err != nil {
		slog.ErrorContext(ctx, "save move transaction failed in make move", "game_id", gameID, "error", err)
		return nil, err
	}

	currentGame.Position = position
	currentGame.Status = status
	currentGame.Result = result
	currentGame.EndReason = endReason
	currentGame.CurrentTurn = nextTurn
	currentGame.WhiteTimeMs = newWhiteTimeMs
	currentGame.BlackTimeMs = newBlackTimeMs
	currentGame.TurnStartedAt = &now
	currentGame.LastMove = move

	if status == StatusFinished {
		s.notifyGameCompleted(ctx, currentGame)
		s.notifyGameStateUpdated(ctx, currentGame)
	}

	slog.InfoContext(ctx, "move completed successfully", "game_id", gameID, "player_id", playerID, "move", move, "status", status)

	return currentGame, nil
}

func (s *Service) ResignGame(
	ctx context.Context,
	gameID string,
	playerID int64,
) (*Game, error) {
	mu := s.getGameLock(gameID)
	mu.Lock()
	defer mu.Unlock()

	s.drawOffers.Delete(gameID)
	s.waitForPendingMoves(gameID)

	currentGame, err := s.Repository.FindGameByID(ctx, gameID)
	if err != nil {
		return nil, err
	}

	if currentGame.Status != StatusActive {
		return nil, errors.New("game is already finished")
	}

	if playerID != currentGame.WhitePlayerID && playerID != currentGame.BlackPlayerID {
		return nil, errors.New("player is not part of this game")
	}

	now := time.Now()
	status := StatusFinished
	endReason := EndReasonResignation

	var result string
	if playerID == currentGame.WhitePlayerID {
		result = ResultBlackWin
	} else {
		result = ResultWhiteWin
	}

	err = s.Repository.UpdateGameState(
		ctx,
		gameID,
		currentGame.Position,
		status,
		result,
		endReason,
		currentGame.CurrentTurn,
		currentGame.WhiteTimeMs,
		currentGame.BlackTimeMs,
		&now,
	)
	if err != nil {
		return nil, err
	}

	currentGame.Status = status
	currentGame.Result = result
	currentGame.EndReason = endReason
	currentGame.TurnStartedAt = &now

	s.notifyGameCompleted(ctx, currentGame)
	s.notifyGameStateUpdated(ctx, currentGame)

	return currentGame, nil
}

func (s *Service) DrawGame(
	ctx context.Context,
	gameID string,
	playerID int64,
) (*Game, error) {
	mu := s.getGameLock(gameID)
	mu.Lock()
	defer mu.Unlock()

	s.drawOffers.Delete(gameID)
	s.waitForPendingMoves(gameID)

	currentGame, err := s.Repository.FindGameByID(ctx, gameID)
	if err != nil {
		return nil, err
	}

	if currentGame.Status != StatusActive {
		return nil, errors.New("game is already finished")
	}

	if playerID != currentGame.WhitePlayerID && playerID != currentGame.BlackPlayerID {
		return nil, errors.New("player is not part of this game")
	}

	now := time.Now()
	status := StatusFinished
	result := ResultDraw
	endReason := EndReasonDrawAgreement

	err = s.Repository.UpdateGameState(
		ctx,
		gameID,
		currentGame.Position,
		status,
		result,
		endReason,
		currentGame.CurrentTurn,
		currentGame.WhiteTimeMs,
		currentGame.BlackTimeMs,
		&now,
	)
	if err != nil {
		return nil, err
	}

	currentGame.Status = status
	currentGame.Result = result
	currentGame.EndReason = endReason
	currentGame.TurnStartedAt = &now

	s.notifyGameCompleted(ctx, currentGame)
	s.notifyGameStateUpdated(ctx, currentGame)

	return currentGame, nil
}

func (s *Service) OfferDraw(
	ctx context.Context,
	gameID string,
	playerID int64,
) (*Game, bool, int64, error) {
	currentGame, err := s.Repository.FindGameByID(ctx, gameID)
	if err != nil {
		return nil, false, 0, err
	}

	if currentGame.Status != StatusActive {
		return nil, false, 0, errors.New("game is already finished")
	}

	if playerID != currentGame.WhitePlayerID && playerID != currentGame.BlackPlayerID {
		return nil, false, 0, errors.New("player is not part of this game")
	}

	val, exists := s.drawOffers.Load(gameID)
	if exists {
		offererID := val.(int64)
		if offererID != playerID {
			s.drawOffers.Delete(gameID)
			g, err := s.DrawGame(ctx, gameID, playerID)
			if err != nil {
				return nil, false, 0, err
			}
			return g, true, offererID, nil
		}
		currentGame.DrawOfferedBy = offererID
		return currentGame, false, offererID, nil
	}

	s.drawOffers.Store(gameID, playerID)
	currentGame.DrawOfferedBy = playerID
	return currentGame, false, playerID, nil
}

func (s *Service) AcceptDraw(
	ctx context.Context,
	gameID string,
	playerID int64,
) (*Game, error) {
	currentGame, err := s.Repository.FindGameByID(ctx, gameID)
	if err != nil {
		return nil, err
	}

	if currentGame.Status != StatusActive {
		return nil, errors.New("game is already finished")
	}

	if playerID != currentGame.WhitePlayerID && playerID != currentGame.BlackPlayerID {
		return nil, errors.New("player is not part of this game")
	}

	val, exists := s.drawOffers.Load(gameID)
	if !exists {
		return nil, errors.New("no pending draw offer to accept")
	}

	offererID := val.(int64)
	if offererID == playerID {
		return nil, errors.New("cannot accept your own draw offer")
	}

	s.drawOffers.Delete(gameID)
	return s.DrawGame(ctx, gameID, playerID)
}

func (s *Service) DeclineDraw(
	ctx context.Context,
	gameID string,
	playerID int64,
) (*Game, error) {
	currentGame, err := s.Repository.FindGameByID(ctx, gameID)
	if err != nil {
		return nil, err
	}

	if playerID != currentGame.WhitePlayerID && playerID != currentGame.BlackPlayerID {
		return nil, errors.New("player is not part of this game")
	}

	s.drawOffers.Delete(gameID)
	currentGame.DrawOfferedBy = 0
	return currentGame, nil
}

func (s *Service) GetGame(
	ctx context.Context,
	gameID string,
) (*Game, error) {
	g, err := s.Repository.FindGameByID(ctx, gameID)
	if err != nil {
		return nil, err
	}
	if val, ok := s.drawOffers.Load(gameID); ok {
		g.DrawOfferedBy = val.(int64)
	}
	moves, err := s.Repository.GetMoves(ctx, gameID)
	if err == nil && len(moves) > 0 {
		g.LastMove = moves[len(moves)-1].Move
	}
	return g, nil
}

func (s *Service) GetMoves(
	ctx context.Context,
	gameID string,
) ([]GameMove, error) {
	return s.Repository.GetMoves(ctx, gameID)
}

func (s *Service) GetPlayerGames(
	ctx context.Context,
	playerID int64,
) ([]Game, error) {
	return s.Repository.GetPlayerGames(ctx, playerID)
}

func (s *Service) isPlayerTurn(
	chessGame *ChessGame,
	currentGame *Game,
	playerID int64,
) bool {
	turn := chessGame.Turn()
	if turn == "w" || turn == "White" {
		return playerID == currentGame.WhitePlayerID
	}
	return playerID == currentGame.BlackPlayerID
}

func (s *Service) EndGameOnDisconnect(
	ctx context.Context,
	gameID string,
	disconnectedPlayerID int64,
) (*Game, error) {
	mu := s.getGameLock(gameID)
	mu.Lock()
	defer mu.Unlock()

	s.drawOffers.Delete(gameID)
	s.waitForPendingMoves(gameID)

	currentGame, err := s.Repository.FindGameByID(ctx, gameID)
	if err != nil {
		return nil, err
	}

	if currentGame.Status != StatusActive {
		return currentGame, nil
	}

	if disconnectedPlayerID != currentGame.WhitePlayerID && disconnectedPlayerID != currentGame.BlackPlayerID {
		return nil, errors.New("player is not part of this game")
	}

	now := time.Now()
	status := StatusFinished
	endReason := EndReasonAbandonment

	var result string
	if disconnectedPlayerID == currentGame.WhitePlayerID {
		result = ResultBlackWin
	} else {
		result = ResultWhiteWin
	}

	newWhiteTimeMs := currentGame.WhiteTimeMs
	newBlackTimeMs := currentGame.BlackTimeMs

	if currentGame.TimeControlMode != ModeDaily {
		var turnStartedAt time.Time
		switch {
		case currentGame.TurnStartedAt != nil:
			turnStartedAt = *currentGame.TurnStartedAt
		case currentGame.StartedAt != nil:
			turnStartedAt = *currentGame.StartedAt
		default:
			turnStartedAt = now
		}

		elapsedMs := now.Sub(turnStartedAt).Milliseconds()
		if elapsedMs < 0 {
			elapsedMs = 0
		}

		isWhiteTurn := currentGame.CurrentTurn == "w" || currentGame.CurrentTurn == "White"
		if isWhiteTurn {
			newWhiteTimeMs -= elapsedMs
			if newWhiteTimeMs < 0 {
				newWhiteTimeMs = 0
			}
		} else {
			newBlackTimeMs -= elapsedMs
			if newBlackTimeMs < 0 {
				newBlackTimeMs = 0
			}
		}
	}

	err = s.Repository.UpdateGameState(
		ctx,
		gameID,
		currentGame.Position,
		status,
		result,
		endReason,
		currentGame.CurrentTurn,
		newWhiteTimeMs,
		newBlackTimeMs,
		&now,
	)
	if err != nil {
		return nil, err
	}

	currentGame.Status = status
	currentGame.Result = result
	currentGame.EndReason = endReason
	currentGame.WhiteTimeMs = newWhiteTimeMs
	currentGame.BlackTimeMs = newBlackTimeMs
	currentGame.TurnStartedAt = &now

	s.notifyGameCompleted(ctx, currentGame)
	s.notifyGameStateUpdated(ctx, currentGame)

	return currentGame, nil
}
