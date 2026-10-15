package bot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/here-arjun-1/Caisaara-backend/internal/stockfish"
)

const enginePoolSize = 4

var (
	validFENRegex = regexp.MustCompile(`^[a-zA-Z0-9/ -]+$`)
	ErrInvalidFEN = errors.New("invalid fen")
)

type Engine struct {
	Pool *stockfish.Pool
}

func NewEngine(path string) *Engine {
	return &Engine{
		Pool: stockfish.NewPool(path, enginePoolSize),
	}
}

func (e *Engine) BestMove(
	ctx context.Context,
	fen string,
	settings EngineSettings,
) (string, error) {
	if fen == "" || !validFENRegex.MatchString(fen) || strings.ContainsAny(fen, "\r\n") {
		return "", ErrInvalidFEN
	}

	process, err := e.Pool.GetProcess(ctx)
	if err != nil {
		return "", fmt.Errorf("get stockfish from pool: %w", err)
	}

	stopWatching := context.AfterFunc(ctx, process.Kill)

	move, err := askForMove(process, fen, settings)

	if !stopWatching() {
		e.Pool.ReplaceBrokenProcess(process)
		return "", fmt.Errorf("stockfish timed out: %w", ctx.Err())
	}
	if err != nil {
		e.Pool.ReplaceBrokenProcess(process)
		return "", err
	}

	e.Pool.ReturnProcess(process)
	return move, nil
}

func (e *Engine) Close() {
	e.Pool.Close()
}

func askForMove(process *stockfish.Process, fen string, settings EngineSettings) (string, error) {
	if fen == "" || !validFENRegex.MatchString(fen) || strings.ContainsAny(fen, "\r\n") {
		return "", ErrInvalidFEN
	}

	commands := []string{
		"ucinewgame",
		fmt.Sprintf("setoption name UCI_LimitStrength value %t", settings.LimitStrength),
	}
	if settings.LimitStrength {
		commands = append(commands, fmt.Sprintf("setoption name UCI_Elo value %d", settings.Elo))
	}
	commands = append(commands,
		fmt.Sprintf("setoption name Skill Level value %d", settings.SkillLevel),
		"isready",
	)

	for _, command := range commands {
		if err := process.Send(command); err != nil {
			return "", err
		}
	}
	if _, err := process.WaitFor("readyok"); err != nil {
		return "", err
	}

	goCommand := fmt.Sprintf("go movetime %d", settings.MoveTimeMs)
	if settings.Depth > 0 {
		goCommand = fmt.Sprintf("go depth %d movetime %d", settings.Depth, settings.MoveTimeMs)
	}

	if err := process.Send("position fen " + fen); err != nil {
		return "", err
	}
	if err := process.Send(goCommand); err != nil {
		return "", err
	}

	line, err := process.WaitFor("bestmove")
	if err != nil {
		return "", err
	}

	fields := strings.Fields(line)
	if len(fields) < 2 || fields[1] == "(none)" {
		return "", ErrNoBestMove
	}

	return fields[1], nil
}
