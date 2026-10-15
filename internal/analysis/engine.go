package analysis

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/stockfish"
)

var (
	ErrEngineNotFound = errors.New("stockfish engine binary not found or failed to start")
	ErrNoBestMove     = errors.New("no valid best move returned by engine")
	ErrTimeout        = errors.New("engine search timed out")
	ErrInvalidInput   = errors.New("invalid engine input")
)

var validUCIInputRegex = regexp.MustCompile(`^[a-zA-Z0-9/ -]+$`)

const analysisPoolSize = 2

type AnalysisEngine struct {
	Pool *stockfish.Pool
}

type PositionOptions struct {
	FEN   string   `json:"fen,omitempty"`
	Moves []string `json:"moves,omitempty"`
}

func (p PositionOptions) Validate() error {
	if p.FEN != "" {
		if !validUCIInputRegex.MatchString(p.FEN) || strings.ContainsAny(p.FEN, "\r\n") {
			return ErrInvalidInput
		}
	}
	for _, m := range p.Moves {
		if m == "" || !validUCIInputRegex.MatchString(m) || strings.ContainsAny(m, "\r\n") {
			return ErrInvalidInput
		}
	}
	return nil
}

func (p PositionOptions) BuildCommand() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if p.FEN != "" && len(p.Moves) > 0 {
		return fmt.Sprintf("position fen %s moves %s", p.FEN, strings.Join(p.Moves, " ")), nil
	}
	if p.FEN != "" {
		return fmt.Sprintf("position fen %s", p.FEN), nil
	}
	if len(p.Moves) > 0 {
		return fmt.Sprintf("position startpos moves %s", strings.Join(p.Moves, " ")), nil
	}
	return "position startpos", nil
}

type AnalysisOptions struct {
	Depth         int           `json:"depth,omitempty"`
	MoveTimeMs    int64         `json:"move_time_ms,omitempty"`
	MultiPV       int           `json:"multipv,omitempty"`
	Threads       int           `json:"threads,omitempty"`
	HashMB        int           `json:"hash_mb,omitempty"`
	SkillLevel    *int          `json:"skill_level,omitempty"`
	Elo           *int          `json:"elo,omitempty"`
	LimitStrength bool          `json:"limit_strength,omitempty"`
	Timeout       time.Duration `json:"timeout,omitempty"`
}

type AnalysisResult struct {
	BestMove             string   `json:"best_move"`
	PonderMove           string   `json:"ponder_move,omitempty"`
	EvaluationCentipawns *int     `json:"evaluation_centipawns,omitempty"`
	MateIn               *int     `json:"mate_in,omitempty"`
	Depth                int      `json:"depth"`
	Nodes                int64    `json:"nodes"`
	PV                   []string `json:"pv,omitempty"`
}

func NewAnalysisEngine(path string) *AnalysisEngine {
	if path == "" {
		path = "stockfish"
	}
	return &AnalysisEngine{
		Pool: stockfish.NewPool(path, analysisPoolSize),
	}
}

func (e *AnalysisEngine) Analyze(ctx context.Context, pos PositionOptions, opts AnalysisOptions) (*AnalysisResult, error) {
	if err := pos.Validate(); err != nil {
		return nil, err
	}

	var cancel context.CancelFunc
	switch {
	case opts.Timeout > 0:
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
	case opts.MoveTimeMs > 0:
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.MoveTimeMs+2000)*time.Millisecond)
	default:
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	}
	defer cancel()

	process, err := e.Pool.GetProcess(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("%w: %w", ErrEngineNotFound, err)
	}

	stopWatching := context.AfterFunc(ctx, process.Kill)

	result, err := runAnalysis(process, pos, opts)

	if !stopWatching() {
		e.Pool.ReplaceBrokenProcess(process)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, ctx.Err()
	}
	if errors.Is(err, ErrNoBestMove) {
		e.Pool.ReturnProcess(process)
		return nil, err
	}
	if err != nil {
		e.Pool.ReplaceBrokenProcess(process)
		return nil, err
	}

	e.Pool.ReturnProcess(process)
	return result, nil
}

func (e *AnalysisEngine) Close() {
	e.Pool.Close()
}

func runAnalysis(process *stockfish.Process, pos PositionOptions, opts AnalysisOptions) (*AnalysisResult, error) {
	threads := 1
	if opts.Threads > 0 {
		threads = opts.Threads
	}
	hashMB := 16
	if opts.HashMB > 0 {
		hashMB = opts.HashMB
	}
	multiPV := 1
	if opts.MultiPV > 0 {
		multiPV = opts.MultiPV
	}
	skillLevel := 20
	if opts.SkillLevel != nil {
		skillLevel = *opts.SkillLevel
	}
	limitStrength := opts.LimitStrength && opts.Elo != nil

	commands := []string{
		"ucinewgame",
		fmt.Sprintf("setoption name Threads value %d", threads),
		fmt.Sprintf("setoption name Hash value %d", hashMB),
		fmt.Sprintf("setoption name MultiPV value %d", multiPV),
		fmt.Sprintf("setoption name UCI_LimitStrength value %t", limitStrength),
		fmt.Sprintf("setoption name Skill Level value %d", skillLevel),
	}
	if limitStrength {
		commands = append(commands, fmt.Sprintf("setoption name UCI_Elo value %d", *opts.Elo))
	}
	commands = append(commands, "isready")

	for _, command := range commands {
		if err := process.Send(command); err != nil {
			return nil, err
		}
	}
	if _, err := process.WaitFor("readyok"); err != nil {
		return nil, fmt.Errorf("readyok failed: %w", err)
	}

	posCmd, err := pos.BuildCommand()
	if err != nil {
		return nil, fmt.Errorf("build position command: %w", err)
	}

	if err := process.Send(posCmd); err != nil {
		return nil, fmt.Errorf("send position command: %w", err)
	}

	var goCmd string
	switch {
	case opts.Depth > 0 && opts.MoveTimeMs > 0:
		goCmd = fmt.Sprintf("go depth %d movetime %d", opts.Depth, opts.MoveTimeMs)
	case opts.Depth > 0:
		goCmd = fmt.Sprintf("go depth %d", opts.Depth)
	case opts.MoveTimeMs > 0:
		goCmd = fmt.Sprintf("go movetime %d", opts.MoveTimeMs)
	default:
		goCmd = "go depth 15"
	}

	if err := process.Send(goCmd); err != nil {
		return nil, fmt.Errorf("send go command: %w", err)
	}

	result := &AnalysisResult{}
	for {
		line, err := process.ReadLine()
		if err != nil {
			return nil, err
		}

		if strings.HasPrefix(line, "info ") {
			parseInfoLine(line, result)
		} else if strings.HasPrefix(line, "bestmove ") {
			parseBestMoveLine(line, result)
			break
		}
	}

	if result.BestMove == "" || result.BestMove == "(none)" {
		return nil, ErrNoBestMove
	}

	return result, nil
}

func parseInfoLine(line string, res *AnalysisResult) {
	fields := strings.Fields(line)
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "depth":
			if i+1 < len(fields) {
				if d, err := strconv.Atoi(fields[i+1]); err == nil {
					res.Depth = d
				}
			}
		case "nodes":
			if i+1 < len(fields) {
				if n, err := strconv.ParseInt(fields[i+1], 10, 64); err == nil {
					res.Nodes = n
				}
			}
		case "score":
			if i+2 < len(fields) {
				scoreType := fields[i+1]
				scoreVal, err := strconv.Atoi(fields[i+2])
				if err == nil {
					switch scoreType {
					case "cp":
						res.EvaluationCentipawns = &scoreVal
						res.MateIn = nil
					case "mate":
						res.MateIn = &scoreVal
						res.EvaluationCentipawns = nil
					}
				}
			}
		case "pv":
			if i+1 < len(fields) {
				res.PV = fields[i+1:]
			}
			return
		}
	}
}

func parseBestMoveLine(line string, res *AnalysisResult) {
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		res.BestMove = fields[1]
	}
	if len(fields) >= 4 && fields[2] == "ponder" {
		res.PonderMove = fields[3]
	}
}
