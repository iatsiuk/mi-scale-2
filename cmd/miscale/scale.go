package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"miscale/internal/ble"
	"miscale/internal/scale"
)

const connectTimeout = 2 * time.Minute

// withScale loads data, connects, runs fn and saves data.
func withScale(cmd *cobra.Command, cfg *rootConfig, fn func(ctx context.Context, e *env, c *connection) error) error {
	e, err := loadEnv(cmd, cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), connectTimeout)
	defer cancel()
	c, err := connect(ctx, e)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := fn(ctx, e, c); err != nil {
		return err
	}
	return e.save()
}

func newScaleCmd(cfg *rootConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "scale", Short: "Scale information and settings"}
	cmd.AddCommand(newScaleInfoCmd(cfg), newScaleUnitCmd(cfg), newScaleSmallObjectCmd(cfg), newScaleEraseCmd(cfg))
	return cmd
}

func newScaleInfoCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show model, firmware and clock (syncs the clock to UTC)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withScale(cmd, cfg, func(ctx context.Context, e *env, c *connection) error {
				t, err := c.sess.Time(ctx)
				if err != nil {
					return err
				}
				i := c.info
				return e.render(fields{
					{"model", i.Model.String()}, {"product_id", i.PnP.ProductID}, {"vendor_id", fmt.Sprintf("%#04x", i.PnP.VendorID)},
					{"product_version", fmt.Sprintf("%#04x", i.PnP.ProductVersion)}, {"software", i.Software}, {"hardware", i.Hardware},
					{"serial", i.Serial}, {"system_id", i.SystemID}, {"clock", t.Local().Format(time.RFC3339)},
				})
			})
		},
	}
}

func newScaleUnitCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "unit kg|lb|jin",
		Short: "Set the unit on the scale display",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := parseUnit(args[0])
			if err != nil || u == scale.UnitSt {
				return errors.New("the scale supports kg, lb and jin")
			}
			return withScale(cmd, cfg, func(ctx context.Context, e *env, c *connection) error {
				if err := c.sess.SetUnit(ctx, u); err != nil {
					return err
				}
				return e.render(fields{{"unit", u.String()}})
			})
		},
	}
}

func newScaleSmallObjectCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "small-object on|off",
		Short: "Toggle weighing of small objects",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			on, err := parseOnOff(args[0])
			if err != nil {
				return err
			}
			return withScale(cmd, cfg, func(ctx context.Context, e *env, c *connection) error {
				if err := c.sess.SetPartMeasure(ctx, on); err != nil {
					return err
				}
				return e.render(fields{{"small_object", on}})
			})
		},
	}
}

func newScaleEraseCmd(cfg *rootConfig) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "erase",
		Short: "Clear all data stored in the scale and restore its settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				return errors.New("this clears all data stored in the scale; rerun with --yes")
			}
			return withScale(cmd, cfg, func(ctx context.Context, e *env, c *connection) error {
				if err := c.sess.EraseHistory(ctx); err != nil {
					return err
				}
				return e.render(fields{{"erased", true}})
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm")
	return cmd
}

func newScanCmd(cfg *rootConfig) *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Print raw scale advertisements",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			a, err := openAdapter(cmd.Context())
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			ch := advertisements(ctx, a, "")
			defer stopScan(cancel, ch)
			return printScan(e, ch)
		},
	}
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 30*time.Second, "scan duration")
	return cmd
}

type scanRow struct {
	Seen            time.Time `json:"seen"`
	ID              string    `json:"id"`
	Value           float64   `json:"value"`
	Unit            string    `json:"unit"`
	Stable          bool      `json:"stable"`
	ImpedanceStable bool      `json:"impedance_stable"`
	Finished        bool      `json:"finished"`
	Part            bool      `json:"part"`
	Overload        bool      `json:"overload"`
	Impedance       uint16    `json:"impedance"`
	Time            time.Time `json:"time"`
}

// printScan streams changed advertisements: markdown rows or json lines.
// It stops at the first write error, e.g. a closed pipe.
func printScan(e *env, ch <-chan advert) error {
	w := &errWriter{w: e.out}
	enc := json.NewEncoder(w)
	if e.format == formatMarkdown {
		mdTable(w, []string{"seen", "id", "weight", "stable", "impedance stable", "finished", "part", "impedance", "time"}, nil)
		if w.err != nil {
			return w.err
		}
	}
	last := map[string]scale.Measurement{}
	for adv := range ch {
		m := adv.m
		if last[adv.id] == m {
			continue
		}
		last[adv.id] = m
		r := scanRow{time.Now(), adv.id, m.Value, m.Unit.String(), m.Stable, m.ImpedanceStable, m.Finished, m.Part, m.Overload, m.Impedance, m.Time.Local()}
		if e.format == formatJSON {
			_ = enc.Encode(r)
		} else {
			_, _ = fmt.Fprintf(w, "| %s | %s | %v %s | %v | %v | %v | %v | %d | %s |\n", r.Seen.Format("15:04:05.000"), r.ID,
				r.Value, r.Unit, r.Stable, r.ImpedanceStable, r.Finished, r.Part, r.Impedance, r.Time.Format(time.RFC3339))
		}
		if w.err != nil {
			return w.err
		}
	}
	return w.err
}

func newGattCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "gatt",
		Short: "Dump GATT characteristics of the scale",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), connectTimeout)
			defer cancel()
			a, err := openAdapter(ctx)
			if err != nil {
				return err
			}
			id, err := waitScale(ctx, a, e.data.Scale.ID)
			if err != nil {
				return err
			}
			d, err := a.Connect(ctx, id)
			if err != nil {
				return err
			}
			defer d.Close()
			return dumpGATT(ctx, e, d)
		},
	}
}

type gattChar struct {
	Service string `json:"service"`
	UUID    string `json:"uuid"`
	Props   string `json:"props"`
	Value   string `json:"value,omitempty"`
	Error   string `json:"error,omitempty"`
}

type gattList []gattChar

func (l gattList) markdown(w io.Writer) {
	rows := make([][]string, len(l))
	for i, c := range l {
		v := c.Value
		if c.Error != "" {
			v = "error: " + c.Error
		}
		rows[i] = []string{c.Service, c.UUID, c.Props, v}
	}
	mdTable(w, []string{"service", "characteristic", "props", "value"}, rows)
}

func dumpGATT(ctx context.Context, e *env, d *ble.Device) error {
	chars := d.Characteristics()
	sort.Slice(chars, func(i, j int) bool {
		if chars[i].Service != chars[j].Service {
			return chars[i].Service < chars[j].Service
		}
		return chars[i].UUID < chars[j].UUID
	})
	out := make(gattList, 0, len(chars))
	for _, c := range chars {
		g := gattChar{Service: c.Service, UUID: c.UUID, Props: c.PropsString()}
		if c.CanRead() {
			if v, err := d.Read(ctx, c); err != nil {
				g.Error = err.Error()
			} else {
				g.Value = hex.EncodeToString(v)
			}
		}
		out = append(out, g)
	}
	return e.render(out)
}
