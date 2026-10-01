package main

import (
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"miscale/internal/app"
	"miscale/internal/store"
)

func newAddCmd(cfg *rootConfig) *cobra.Command {
	var userName, at string
	var weight float64
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a manual weight record",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if weight <= 0 {
				return errors.New("--weight must be positive")
			}
			t := time.Now().Truncate(time.Second)
			if at != "" {
				var err error
				if t, err = time.Parse(time.RFC3339, at); err != nil {
					return err
				}
			}
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			u, err := e.data.User(userName)
			if err != nil {
				return err
			}
			r := store.Record{UserID: u.ID, Time: t, Kind: store.KindManual, Origin: store.OriginManual, WeightKg: weight}
			if err := app.Analyze(&r, *u); err != nil {
				return err
			}
			r = e.data.Add(&r)
			if err := e.save(); err != nil {
				return err
			}
			return e.render(detailView(e.data, &r))
		},
	}
	cmd.Flags().StringVar(&userName, "user", "", "family member")
	cmd.Flags().Float64Var(&weight, "weight", 0, "weight in kg")
	cmd.Flags().StringVar(&at, "time", "", "RFC3339 time (default: now)")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("weight")
	return cmd
}

func newHistoryCmd(cfg *rootConfig) *cobra.Command {
	var userName string
	var limit int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "List records, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			recs, err := filterRecords(e.data, cmd.Flags().Changed("user"), userName)
			if err != nil {
				return err
			}
			if limit > 0 && len(recs) > limit {
				recs = recs[:limit]
			}
			out := make(recordList, len(recs))
			for i, r := range recs {
				out[i] = detailView(e.data, r)
			}
			return e.render(out)
		},
	}
	cmd.Flags().StringVar(&userName, "user", "", "family member; empty string lists unassigned records")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "number of records, 0 for all")
	return cmd
}

// filterRecords returns records newest first: all, one user's, or unassigned
// ones for an empty name.
func filterRecords(d *store.Data, byUser bool, name string) ([]*store.Record, error) {
	userID := ""
	if byUser && name != "" {
		u, err := d.User(name)
		if err != nil {
			return nil, err
		}
		userID = u.ID
	}
	var recs []*store.Record
	for i := range d.Records {
		if r := &d.Records[i]; !byUser || r.UserID == userID {
			recs = append(recs, r)
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Time.After(recs[j].Time) })
	return recs, nil
}

func recordArg(e *env, s string) (*store.Record, error) {
	id, err := strconv.Atoi(s)
	if err != nil {
		return nil, err
	}
	return e.data.Record(id)
}

func newShowCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "show ID",
		Short: "Show a record with body composition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			r, err := recordArg(e, args[0])
			if err != nil {
				return err
			}
			return e.render(detailView(e.data, r))
		},
	}
}

func newAssignCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "assign ID NAME",
		Short: "Give a record to a family member and analyze it",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			r, err := recordArg(e, args[0])
			if err != nil {
				return err
			}
			if err := app.Assign(e.data, r.ID, args[1]); err != nil {
				return err
			}
			if err := e.save(); err != nil {
				return err
			}
			return e.render(detailView(e.data, r))
		},
	}
}

func newRmCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "rm ID",
		Short: "Delete a record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			r, err := recordArg(e, args[0])
			if err != nil {
				return err
			}
			id := r.ID
			e.data.Remove(id)
			if err := e.save(); err != nil {
				return err
			}
			return e.render(fields{{"removed_record", id}})
		},
	}
}
