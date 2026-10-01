package main

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"miscale/internal/scale"
	"miscale/internal/store"
)

const envDataPath = "MISCALE_DATA"

var (
	errNoChoice = errors.New("no choice")
	errNoAnswer = errors.New("no answer")
)

// stdinIsTTY reports whether stdin is a terminal; replaceable in tests.
var stdinIsTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) //nolint:gosec // fd fits in int
}

type rootConfig struct {
	dataPath string
	format   string
}

func newRootCmd() *cobra.Command {
	return buildRootCmd(&rootConfig{})
}

func buildRootCmd(cfg *rootConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "miscale",
		Short:         "Xiaomi Mi Body Composition Scale 2 client",
		Long:          "Weigh, analyze body composition and manage the Xiaomi Mi Body Composition Scale 2 like Zepp Life does.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return validFormat(cfg.format)
		},
	}
	cmd.PersistentFlags().StringVarP(&cfg.format, "format", "f", formatMarkdown, "output format: markdown or json")
	cmd.PersistentFlags().StringVar(&cfg.dataPath, "data", "",
		"data file (env "+envDataPath+", default ~/Library/Application Support/miscale/data.json)")
	cmd.AddCommand(
		newMeasureCmd(cfg), newGuestCmd(cfg), newBabyCmd(cfg), newBalanceCmd(cfg), newSyncCmd(cfg),
		newScaleCmd(cfg), newUserCmd(cfg), newAddCmd(cfg), newHistoryCmd(cfg), newShowCmd(cfg),
		newAssignCmd(cfg), newRmCmd(cfg), newConfigCmd(cfg), newScanCmd(cfg), newGattCmd(cfg),
	)
	return cmd
}

// env is the state of one command run. Results go to out in the chosen
// format; progress and prompts go to log so json output stays clean.
type env struct {
	path        string
	data        *store.Data
	in          *bufio.Reader
	out         io.Writer
	log         io.Writer
	format      string
	interactive bool
}

func loadEnv(cmd *cobra.Command, cfg *rootConfig) (*env, error) {
	path := cfg.dataPath
	if path == "" {
		path = os.Getenv(envDataPath)
	}
	if path == "" {
		var err error
		if path, err = store.DefaultPath(); err != nil {
			return nil, err
		}
	}
	d, err := store.Load(path, randomUID)
	if err != nil {
		return nil, err
	}
	return &env{
		path: path, data: d, format: cfg.format, interactive: stdinIsTTY(),
		in: bufio.NewReader(cmd.InOrStdin()), out: cmd.OutOrStdout(), log: cmd.ErrOrStderr(),
	}, nil
}

func (e *env) save() error { return store.Save(e.path, e.data) }

func (e *env) printf(format string, args ...any) { _, _ = fmt.Fprintf(e.log, format, args...) }

// ask reads one answer line; closed input or a missing terminal is
// errNoAnswer, which callers must not treat as a refusal.
func (e *env) ask(prompt string) (string, error) {
	if !e.interactive {
		return "", errNoAnswer
	}
	e.printf("%s", prompt)
	s, err := e.in.ReadString('\n')
	if err != nil {
		e.printf("\n")
		return "", errNoAnswer
	}
	return strings.TrimSpace(s), nil
}

// confirm asks until the answer is yes, no or empty (no); a typo is asked
// again because a no may discard data.
func (e *env) confirm(prompt string) (bool, error) {
	for {
		s, err := e.ask(prompt + " [y/N] ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(s) {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		}
	}
}

// choose prints numbered options and returns the picked index; an empty
// answer is errNoChoice, an invalid one is asked again.
func (e *env) choose(prompt string, options []string) (int, error) {
	if !e.interactive || len(options) == 0 {
		return 0, errNoAnswer
	}
	for i, o := range options {
		e.printf("  %d) %s\n", i+1, o)
	}
	for {
		s, err := e.ask(prompt + " (number, empty to discard): ")
		if err != nil {
			return 0, err
		}
		if s == "" {
			return 0, errNoChoice
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
	}
}

func randomUID() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.LittleEndian.Uint32(b[:]) | 1
}

func parseUnit(s string) (scale.Unit, error) {
	switch strings.ToLower(s) {
	case "kg":
		return scale.UnitKg, nil
	case "lb":
		return scale.UnitLb, nil
	case "jin":
		return scale.UnitJin, nil
	case "st":
		return scale.UnitSt, nil
	}
	return 0, fmt.Errorf("unknown unit %q", s)
}

func parseOnOff(s string) (bool, error) {
	switch s {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, fmt.Errorf("expected on or off, got %q", s)
}
