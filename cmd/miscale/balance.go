package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"miscale/internal/scale"
	"miscale/internal/store"
)

func newBalanceCmd(cfg *rootConfig) *cobra.Command {
	var userName string
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "One-foot balance test",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			var user *store.User
			if userName != "" {
				if user, err = e.data.User(userName); err != nil {
					return err
				}
			}
			secs, err := runBalance(cmd.Context(), e)
			if err != nil {
				return err
			}
			return saveBalance(e, user, secs)
		},
	}
	cmd.Flags().StringVar(&userName, "user", "", "family member (default: ask)")
	return cmd
}

func runBalance(ctx context.Context, e *env) (float64, error) {
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	c, err := connect(connectCtx, e)
	cancel()
	if err != nil {
		return 0, err
	}
	defer c.Close()
	e.printf("stand on one foot with your eyes closed; Ctrl-C stops the test\n")
	var last scale.OneFoot
	err = c.sess.OneFoot(ctx, func(p scale.OneFoot) {
		last = p
		e.printf("\r%5.1f s", p.Duration.Seconds())
	})
	e.printf("\n")
	if err != nil && !errors.Is(err, context.Canceled) {
		return 0, err
	}
	return last.Duration.Seconds(), nil
}

func saveBalance(e *env, user *store.User, secs float64) error {
	var userID string
	if user != nil {
		userID = user.ID
	} else {
		names := make([]string, len(e.data.Users))
		for i, u := range e.data.Users {
			names[i] = u.Name
		}
		i, err := e.choose("whose result is it?", names)
		switch {
		case errors.Is(err, errNoAnswer):
			e.printf("the result stays unassigned: miscale assign ID NAME\n")
		case err != nil:
			return e.render(fields{{"balance_seconds", round(secs, 1)}, {"saved", false}})
		default:
			userID = e.data.Users[i].ID
		}
	}
	r := e.data.Add(&store.Record{UserID: userID, Time: time.Now(), Kind: store.KindBalance, Origin: store.OriginLive, BalanceSeconds: secs})
	if err := e.save(); err != nil {
		return err
	}
	return e.render(detailView(e.data, &r))
}

func balanceLevel(l int) string {
	if l < 0 {
		return "n/a (ages 20..69 only)"
	}
	return fmt.Sprintf("%d of 5", l)
}
