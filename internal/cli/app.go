package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"offline-sync-agent/internal/config"
	"offline-sync-agent/internal/models"
	syncer "offline-sync-agent/internal/sync"
)

type queueRepository interface {
	AddOperation(ctx context.Context, op models.Operation) error
	ListOperations(ctx context.Context) ([]models.Operation, error)
	ListConflicts(ctx context.Context) ([]models.ConflictRecord, error)
	ResolveConflict(ctx context.Context, id string) error
	CountUnsynced(ctx context.Context) (int, error)
}

type syncRunner interface {
	SyncNow(ctx context.Context) (syncer.Summary, error)
	Run(ctx context.Context, interval time.Duration, maxBackoff time.Duration) error
}

type App struct {
	cfg        config.ClientConfig
	queue      queueRepository
	syncer     syncRunner
	logger     *slog.Logger
	stdout     io.Writer
	stderr     io.Writer
	executable string
}

func NewApp(cfg config.ClientConfig, queue queueRepository, syncer syncRunner, logger *slog.Logger, stdout io.Writer, stderr io.Writer) *App {
	return &App{
		cfg:        cfg,
		queue:      queue,
		syncer:     syncer,
		logger:     logger,
		stdout:     stdout,
		stderr:     stderr,
		executable: filepath.Base(os.Args[0]),
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		a.printHelp()
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		a.printHelp()
		return nil
	case "add":
		return a.runAdd(ctx, args[1:])
	case "delete":
		return a.runDelete(ctx, args[1:])
	case "status":
		return a.runStatus(ctx, args[1:])
	case "conflicts":
		return a.runConflicts(ctx, args[1:])
	case "resolve":
		return a.runResolve(ctx, args[1:])
	case "debug":
		return a.runDebug(ctx, args[1:])
	case "sync":
		return a.runSync(ctx, args[1:])
	default:
		a.printHelp()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (a *App) runAdd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var (
		data     string
		priority int
	)

	fs.StringVar(&data, "data", "", "operation payload")
	fs.IntVar(&priority, "priority", models.HighPriority, "lower numbers are processed first")

	if err := fs.Parse(args); err != nil {
		return swallowHelp(err)
	}

	if data == "" && fs.NArg() > 0 {
		data = fs.Arg(0)
	}
	if data == "" {
		data = "sample data"
	}

	op := models.Operation{
		ID:        uuid.NewString(),
		Type:      models.CREATE,
		Data:      data,
		Timestamp: time.Now().Unix(),
		Version:   int(time.Now().Unix()),
		Priority:  priority,
	}

	if err := a.queue.AddOperation(ctx, op); err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "Queued CREATE operation %s\n", op.ID)
	return nil
}

func (a *App) runDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var (
		id       string
		priority int
	)

	fs.StringVar(&id, "id", "", "record identifier to delete")
	fs.IntVar(&priority, "priority", models.HighPriority, "lower numbers are processed first")

	if err := fs.Parse(args); err != nil {
		return swallowHelp(err)
	}

	if id == "" && fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	if id == "" {
		return fmt.Errorf("delete requires an operation id")
	}

	op := models.Operation{
		ID:        id,
		Type:      models.DELETE,
		Timestamp: time.Now().Unix(),
		Version:   int(time.Now().Unix()),
		Priority:  priority,
	}

	if err := a.queue.AddOperation(ctx, op); err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "Queued DELETE operation for %s\n", id)
	return nil
}

func (a *App) runStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	if err := fs.Parse(args); err != nil {
		return swallowHelp(err)
	}

	operations, err := a.queue.ListOperations(ctx)
	if err != nil {
		return err
	}

	if len(operations) == 0 {
		fmt.Fprintln(a.stdout, "No queued operations.")
		return nil
	}

	writer := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tTYPE\tVERSION\tPRIORITY\tSYNCED\tTIMESTAMP")
	for _, op := range operations {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%d\t%d\t%t\t%d\n",
			op.ID,
			op.Type,
			op.Version,
			op.Priority,
			op.Synced,
			op.Timestamp,
		)
	}

	return writer.Flush()
}

func (a *App) runConflicts(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("conflicts", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	if err := fs.Parse(args); err != nil {
		return swallowHelp(err)
	}

	conflicts, err := a.queue.ListConflicts(ctx)
	if err != nil {
		return err
	}

	if len(conflicts) == 0 {
		fmt.Fprintln(a.stdout, "No conflicts recorded.")
		return nil
	}

	writer := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tVERSION\tSTATUS\tSTRATEGY\tTIMESTAMP")
	for _, conflict := range conflicts {
		fmt.Fprintf(
			writer,
			"%s\t%d\t%s\t%s\t%d\n",
			conflict.ID,
			conflict.Version,
			conflict.Status,
			conflict.Strategy,
			conflict.Timestamp,
		)
	}

	return writer.Flush()
}

func (a *App) runResolve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var id string
	fs.StringVar(&id, "id", "", "conflict identifier to resolve")

	if err := fs.Parse(args); err != nil {
		return swallowHelp(err)
	}

	if id == "" && fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	if id == "" {
		return fmt.Errorf("resolve requires a conflict id")
	}

	if err := a.queue.ResolveConflict(ctx, id); err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "Marked conflict %s as resolved\n", id)
	return nil
}

func (a *App) runDebug(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("debug", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	if err := fs.Parse(args); err != nil {
		return swallowHelp(err)
	}

	pending, err := a.queue.CountUnsynced(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "Server URL:\t%s\n", fallback(a.cfg.ServerURL, "<not configured>"))
	fmt.Fprintf(a.stdout, "Auth token:\t%s\n", configured(a.cfg.AuthToken))
	fmt.Fprintf(a.stdout, "Database path:\t%s\n", a.cfg.DBPath)
	fmt.Fprintf(a.stdout, "Pending ops:\t%d\n", pending)
	return nil
}

func (a *App) runSync(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var (
		once       bool
		interval   time.Duration
		maxBackoff time.Duration
	)

	fs.BoolVar(&once, "once", false, "run a single sync cycle and exit")
	fs.DurationVar(&interval, "interval", a.cfg.SyncInterval, "delay between successful sync cycles")
	fs.DurationVar(&maxBackoff, "max-backoff", a.cfg.MaxBackoff, "maximum retry backoff after failures")

	if err := fs.Parse(args); err != nil {
		return swallowHelp(err)
	}

	if err := a.cfg.ValidateSync(); err != nil {
		return err
	}

	if once {
		summary, err := a.syncer.SyncNow(ctx)
		if err != nil {
			return err
		}

		a.printSyncSummary(summary)
		return nil
	}

	fmt.Fprintln(a.stdout, "Starting sync loop. Press Ctrl+C to stop.")
	return a.syncer.Run(ctx, interval, maxBackoff)
}

func (a *App) printHelp() {
	fmt.Fprintf(a.stdout, "Usage: %s <command> [options]\n\n", a.executable)
	fmt.Fprintln(a.stdout, "Commands:")
	fmt.Fprintln(a.stdout, "  add        Queue a CREATE operation")
	fmt.Fprintln(a.stdout, "  delete     Queue a DELETE operation")
	fmt.Fprintln(a.stdout, "  status     Show queued operations")
	fmt.Fprintln(a.stdout, "  conflicts  Show recorded sync conflicts")
	fmt.Fprintln(a.stdout, "  resolve    Mark a conflict as resolved")
	fmt.Fprintln(a.stdout, "  debug      Print local configuration and queue details")
	fmt.Fprintln(a.stdout, "  sync       Run sync once or in a background loop")
	fmt.Fprintf(a.stdout, "\nRun \"%s <command> -h\" for command-specific help.\n", a.executable)
}

func (a *App) printSyncSummary(summary syncer.Summary) {
	fmt.Fprintf(
		a.stdout,
		"Sync complete: synced=%d conflicts=%d pulled=%d quality=%s duration=%s\n",
		summary.Synced,
		summary.Conflicts,
		summary.Pulled,
		summary.NetworkQuality,
		summary.Duration,
	)
}

func swallowHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}

	return err
}

func configured(value string) string {
	if value == "" {
		return "missing"
	}

	return "configured"
}

func fallback(value string, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}
