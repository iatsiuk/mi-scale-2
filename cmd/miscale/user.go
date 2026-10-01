package main

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"miscale/internal/app"
	"miscale/internal/bodycomp"
	"miscale/internal/store"
)

func newUserCmd(cfg *rootConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage family members"}
	cmd.AddCommand(newUserListCmd(cfg), newUserAddCmd(cfg), newUserEditCmd(cfg), newUserRmCmd(cfg))
	return cmd
}

type userView struct {
	Name     string  `json:"name"`
	Sex      string  `json:"sex"`
	Birth    string  `json:"birth"`
	HeightCm int     `json:"height_cm"`
	LastKg   float64 `json:"last_weight_kg"`

	last string
}

type userList []userView

func (l userList) markdown(w io.Writer) {
	rows := make([][]string, len(l))
	for i, u := range l {
		rows[i] = []string{u.Name, u.Sex, u.Birth, strconv.Itoa(u.HeightCm) + " cm", u.last}
	}
	mdTable(w, []string{"name", "sex", "birth", "height", "last weight"}, rows)
}

func sexString(s bodycomp.Sex) string {
	if s == bodycomp.Male {
		return "m"
	}
	return "f"
}

func newUserListCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List family members",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			out := make(userList, 0, len(e.data.Users))
			for _, u := range e.data.Users {
				kg := app.LastWeight(e.data, u)
				out = append(out, userView{u.Name, sexString(u.Sex), u.Birth, u.HeightCm, kg, app.FormatWeight(kg, e.data.DisplayUnit)})
			}
			return e.render(out)
		},
	}
}

type userFlags struct {
	name, sex, birth string
	height           int
	weight           float64
}

func (f *userFlags) register(cmd *cobra.Command, nameHelp string) {
	cmd.Flags().StringVar(&f.name, "name", "", nameHelp)
	cmd.Flags().StringVar(&f.sex, "sex", "", "m or f")
	cmd.Flags().StringVar(&f.birth, "birth", "", "birth month YYYY-MM")
	cmd.Flags().IntVar(&f.height, "height", 0, "height in cm (30..242)")
	cmd.Flags().Float64Var(&f.weight, "weight", 0, "current weight in kg, used to match the first measurement")
}

// apply sets the given fields on u, validating them like the app forms.
func (f *userFlags) apply(u *store.User) error {
	if f.sex != "" {
		s, err := parseSex(f.sex)
		if err != nil {
			return err
		}
		u.Sex = s
	}
	if f.birth != "" {
		u.Birth = f.birth
		if _, _, ok := u.BirthYearMonth(); !ok {
			return errors.New("--birth must be YYYY-MM")
		}
	}
	if f.height != 0 {
		if f.height < 30 || f.height > 242 {
			return errors.New("--height must be 30..242 cm")
		}
		u.HeightCm = f.height
	}
	if f.weight < 0 {
		return errors.New("--weight must be positive")
	}
	if f.weight > 0 {
		u.WeightKg = f.weight
	}
	return nil
}

func saveUser(e *env, u store.User) error {
	y, m, _ := u.BirthYearMonth()
	if age := bodycomp.AgeAt(y, m, time.Now()); age >= 6 && age < 18 {
		e.printf("note: for minors it is recommended to pay attention to weight changes only\n")
	}
	if err := e.save(); err != nil {
		return err
	}
	kg := app.LastWeight(e.data, u)
	return e.render(userList{{u.Name, sexString(u.Sex), u.Birth, u.HeightCm, kg, app.FormatWeight(kg, e.data.DisplayUnit)}})
}

func newUserAddCmd(cfg *rootConfig) *cobra.Command {
	var f userFlags
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a family member",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.name == "" || f.sex == "" || f.birth == "" || f.height == 0 || f.weight == 0 {
				return errors.New("user add needs --name --sex --birth --height --weight")
			}
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			if _, err := e.data.User(f.name); err == nil {
				return fmt.Errorf("user %q exists", f.name)
			}
			u := store.User{ID: strconv.FormatInt(time.Now().UnixMilli(), 10), Name: f.name}
			if err := f.apply(&u); err != nil {
				return err
			}
			e.data.Users = append(e.data.Users, u)
			return saveUser(e, u)
		},
	}
	f.register(cmd, "name")
	return cmd
}

func newUserEditCmd(cfg *rootConfig) *cobra.Command {
	var f userFlags
	cmd := &cobra.Command{
		Use:   "edit NAME",
		Short: "Change a family member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			u, err := e.data.User(args[0])
			if err != nil {
				return err
			}
			if f.name != "" {
				if other, err := e.data.User(f.name); err == nil && other.ID != u.ID {
					return fmt.Errorf("user %q exists", f.name)
				}
				u.Name = f.name
			}
			if err := f.apply(u); err != nil {
				return err
			}
			return saveUser(e, *u)
		},
	}
	f.register(cmd, "new name")
	return cmd
}

func newUserRmCmd(cfg *rootConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "rm NAME",
		Short: "Remove a family member and their records",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			u, err := e.data.User(args[0])
			if err != nil {
				return err
			}
			id, name := u.ID, u.Name
			removed := e.data.RemoveUser(id)
			if err := e.save(); err != nil {
				return err
			}
			return e.render(fields{{"removed_user", name}, {"removed_records", removed}})
		},
	}
}
