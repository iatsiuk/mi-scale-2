package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"miscale/internal/app"
	"miscale/internal/scale"
)

func newSyncCmd(cfg *rootConfig) *cobra.Command {
	var noAck bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Download offline records from the scale",
		Long: "Download records the scale stored for this client's history key. After the records are saved\n" +
			"the scale is told to delete them, unless --no-ack is given.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withScale(cmd, cfg, func(ctx context.Context, e *env, c *connection) error {
				return syncHistory(ctx, e, c, !noAck)
			})
		},
	}
	cmd.Flags().BoolVar(&noAck, "no-ack", false, "keep the records on the scale")
	return cmd
}

func syncHistory(ctx context.Context, e *env, c *connection, ack bool) error {
	ms, received, err := c.sess.History(ctx, e.data.UID)
	complete := err == nil
	if err != nil && !errors.Is(err, scale.ErrIncompleteHistory) {
		return err
	}
	if !complete {
		e.printf("warning: %v (records stay on the scale)\n", err)
	}
	res := app.ImportHistory(e.data, ms, time.Now())
	if res.Unassigned > 0 {
		e.printf("assign unassigned records with: miscale history --user '' and miscale assign ID NAME\n")
	}
	// the scale deletes acknowledged records, so ack only what is stored
	if err := e.save(); err != nil {
		return err
	}
	acked := false
	if complete && received > 0 && ack {
		if err := c.sess.AckHistory(ctx, e.data.UID); err != nil {
			return fmt.Errorf("records saved, but the scale did not take the acknowledgement: %w", err)
		}
		acked = true
	}
	return e.render(fields{{"received", received}, {"valid", len(ms)}, {"added", res.Added}, {"unassigned", res.Unassigned},
		{"skipped", res.Skipped}, {"complete", complete}, {"acknowledged", acked}})
}
