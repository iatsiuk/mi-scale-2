package main

import (
	"strconv"

	"github.com/spf13/cobra"

	"miscale/internal/store"
)

type configFlags struct {
	merge, unit, uid string
	forget           bool
}

func newConfigCmd(cfg *rootConfig) *cobra.Command {
	var f configFlags
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			if err := f.apply(e.data); err != nil {
				return err
			}
			if err := e.save(); err != nil {
				return err
			}
			d := e.data
			return e.render(fields{{"data", e.path}, {"scale_id", d.Scale.ID}, {"scale_serial", d.Scale.Serial},
				{"uid", d.UID}, {"merge", d.Merge}, {"unit", d.DisplayUnit.String()}})
		},
	}
	cmd.Flags().StringVar(&f.merge, "merge", "", "on|off: merge weigh-ins by the same user within 30 s")
	cmd.Flags().StringVar(&f.unit, "unit", "", "display unit kg|lb|jin|st")
	cmd.Flags().StringVar(&f.uid, "uid", "", "32-bit history key on the scale (Zepp Life uses the account id)")
	cmd.Flags().BoolVar(&f.forget, "forget-scale", false, "forget the paired scale")
	return cmd
}

func (f configFlags) apply(d *store.Data) error {
	if f.merge != "" {
		on, err := parseOnOff(f.merge)
		if err != nil {
			return err
		}
		d.Merge = on
	}
	if f.unit != "" {
		u, err := parseUnit(f.unit)
		if err != nil {
			return err
		}
		d.DisplayUnit = u
	}
	if f.uid != "" {
		v, err := strconv.ParseUint(f.uid, 0, 32)
		if err != nil {
			return err
		}
		d.UID = uint32(v)
	}
	if f.forget {
		d.Scale = store.Scale{}
	}
	return nil
}
