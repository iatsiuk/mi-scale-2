package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"miscale/internal/app"
	"miscale/internal/ble"
	"miscale/internal/bodycomp"
	"miscale/internal/scale"
	"miscale/internal/store"
)

const steppedOffMsg = "wait until the analysis is completed before stepping off the scale"

// weigh runs the live state machine over advertisements until a final event.
func weigh(ctx context.Context, e *env, timeout time.Duration) (app.Event, error) {
	a, err := openAdapter(ctx)
	if err != nil {
		return app.Event{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	ch := advertisements(ctx, a, e.data.Scale.ID)
	defer stopScan(cancel, ch)
	e.printf("step on the scale...\n")
	tr := app.NewLiveTracker()
	for adv := range ch {
		for _, ev := range tr.Feed(adv.m, time.Now()) {
			if final := progress(e, ev); final {
				if e.data.Scale.ID == "" {
					e.data.Scale.ID = adv.id
				}
				return ev, nil
			}
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return app.Event{}, errors.New("no measurement: timeout")
	}
	return app.Event{}, ctx.Err()
}

// progress prints an intermediate event and reports whether ev is final.
func progress(e *env, ev app.Event) bool {
	m := ev.Measurement
	switch ev.Kind {
	case app.EventWeight:
		state := "measuring"
		if m.Stable {
			state = "stable"
		}
		e.printf("  %v %s %s\n", m.Value, m.Unit, state)
	case app.EventMeasuringImpedance:
		e.printf("  measuring body composition, stay barefoot on the scale...\n")
	case app.EventOverload:
		e.printf("  weight exceeds the measurement range (max %s)\n", app.FormatWeight(150, m.Unit))
	default:
		return true
	}
	return false
}

func newMeasureCmd(cfg *rootConfig) *cobra.Command {
	var userName string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "measure",
		Short: "Weigh and analyze body composition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			if len(e.data.Users) == 0 {
				return errors.New("no users: add one with miscale user add")
			}
			ev, err := weigh(cmd.Context(), e, timeout)
			if err != nil {
				return err
			}
			return saveLive(e, ev, userName)
		},
	}
	cmd.Flags().StringVar(&userName, "user", "", "family member (default: matched by weight)")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 2*time.Minute, "how long to wait")
	return cmd
}

// saveLive handles a final live event like HMWeightingActivity.
func saveLive(e *env, ev app.Event, userName string) error {
	m := ev.Measurement
	if done, err := unsavedLive(e, ev); done {
		return err
	}
	userID, err := e.pickUser(userName, m.WeightKg())
	if errors.Is(err, errNoChoice) {
		return discard(e, m)
	}
	if err != nil {
		return err
	}
	r, ok, err := app.SaveMeasurement(e.data, m, userID, store.OriginLive, ev.WithComposition)
	if err != nil {
		return err
	}
	if !ok {
		e.printf("this measurement is already saved\n")
		return nil
	}
	if err := e.save(); err != nil {
		return err
	}
	return e.render(detailView(e.data, &r))
}

// unsavedLive ends a live result that is not saved: stepped off, small
// object, or a declined weight-only record.
func unsavedLive(e *env, ev app.Event) (bool, error) {
	m := ev.Measurement
	switch ev.Kind {
	case app.EventSteppedOff:
		return true, errors.New(steppedOffMsg)
	case app.EventSmallObject:
		return true, e.render(fields{{"small_object", true}, {"weight", m.Value}, {"unit", m.Unit.String()}, {"saved", false}})
	case app.EventImpedanceFailed:
		e.printf("weight %s, body composition could not be measured (no socks, stand still)\n", app.FormatWeight(m.WeightKg(), m.Unit))
		// only an explicit no discards; without an answer the weight is kept
		if ok, err := e.confirm("save the weight only?"); err == nil && !ok {
			return true, discard(e, m)
		}
	}
	return false, nil
}

// discard drops a live result; the history sync must not import it again.
func discard(e *env, m scale.Measurement) error {
	e.data.Ignore(m.Time)
	e.printf("measurement discarded\n")
	return e.save()
}

// pickUser resolves --user, or matches by weight and asks when ambiguous.
// It returns an empty id, an unassigned record, when nobody can answer;
// errNoChoice means the user chose to discard the measurement.
func (e *env) pickUser(name string, kg float64) (string, error) {
	if name != "" {
		u, err := e.data.User(name)
		if err != nil {
			return "", err
		}
		return u.ID, nil
	}
	if matched := app.Match(e.data, kg); len(matched) == 1 {
		return matched[0].ID, nil
	}
	cands := app.Candidates(e.data, kg)
	names := make([]string, len(cands))
	for i, u := range cands {
		names[i] = u.Name
	}
	if e.interactive && len(cands) > 0 {
		e.printf("whose measurement is it?\n")
	}
	i, err := e.choose("user", names)
	if errors.Is(err, errNoAnswer) {
		e.printf("no user matched, the record stays unassigned: miscale assign ID NAME\n")
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return cands[i].ID, nil
}

func newGuestCmd(cfg *rootConfig) *cobra.Command {
	var sex, birth string
	var height int
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "guest",
		Short: "Weigh a guest without saving",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			guest, err := newGuest(sex, birth, height)
			if err != nil {
				return err
			}
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			ev, err := weigh(cmd.Context(), e, timeout)
			if err != nil {
				return err
			}
			return showGuest(e, ev, guest)
		},
	}
	cmd.Flags().StringVar(&sex, "sex", "", "m or f")
	cmd.Flags().IntVar(&height, "height", 0, "height in cm")
	cmd.Flags().StringVar(&birth, "birth", "", "birth month YYYY-MM")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 2*time.Minute, "how long to wait")
	return cmd
}

func newGuest(sex, birth string, height int) (store.User, error) {
	s, err := parseSex(sex)
	if err != nil {
		return store.User{}, err
	}
	g := store.User{ID: "guest", Name: "guest", Sex: s, Birth: birth, HeightCm: height}
	if _, _, ok := g.BirthYearMonth(); !ok || height <= 0 {
		return store.User{}, errors.New("--height and --birth YYYY-MM are required")
	}
	return g, nil
}

func showGuest(e *env, ev app.Event, guest store.User) error { //nolint:gocritic // one call per run
	m := ev.Measurement
	switch ev.Kind {
	case app.EventSteppedOff:
		return errors.New(steppedOffMsg)
	case app.EventSmallObject:
		return e.render(fields{{"small_object", true}, {"weight", m.Value}, {"unit", m.Unit.String()}})
	}
	r := store.Record{Time: m.Time, Kind: store.KindScale, Origin: store.OriginLive, WeightKg: m.WeightKg(), Unit: m.Unit,
		Impedance: int(m.Impedance), ImpedanceStable: ev.WithComposition && m.ImpedanceStable}
	if err := app.Analyze(&r, guest); err != nil {
		return err
	}
	e.data.Ignore(m.Time) // guest weighings are never imported from the history
	if err := e.save(); err != nil {
		return err
	}
	v := newRecordView(e.data, &r, m.Unit).withMetrics(&r, &guest, m.Unit)
	v.User = guest.Name
	return e.render(v)
}

func newBabyCmd(cfg *rootConfig) *cobra.Command {
	var babyName, adultName string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "baby",
		Short: "Weigh an infant: the adult alone, then holding the baby",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv(cmd, cfg)
			if err != nil {
				return err
			}
			baby, err := e.data.User(babyName)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			adult, both, err := weighTwice(ctx, e)
			if err != nil {
				return err
			}
			return saveBaby(e, baby, adultName, adult, both)
		},
	}
	cmd.Flags().StringVar(&babyName, "baby", "", "infant family member")
	cmd.Flags().StringVar(&adultName, "adult", "", "adult family member (default: matched by weight)")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 3*time.Minute, "how long to wait")
	_ = cmd.MarkFlagRequired("baby")
	return cmd
}

// weighStable waits for a stable weight with a timestamp different from skip.
func weighStable(ctx context.Context, a *ble.Adapter, e *env, skip time.Time) (scale.Measurement, error) {
	ctx, cancel := context.WithCancel(ctx)
	ch := advertisements(ctx, a, e.data.Scale.ID)
	defer stopScan(cancel, ch) // the next weighing reuses the adapter
	for adv := range ch {
		m := adv.m
		if m.Stable && !m.Overload && m.Value > 0 && !m.Time.Equal(skip) {
			return m, nil
		}
	}
	return scale.Measurement{}, errors.New("no stable weight: timeout")
}

func weighTwice(ctx context.Context, e *env) (adult, both scale.Measurement, err error) {
	a, err := openAdapter(ctx)
	if err != nil {
		return adult, both, err
	}
	e.printf("1/2: step on the scale alone\n")
	if adult, err = weighStable(ctx, a, e, time.Time{}); err != nil {
		return adult, both, err
	}
	e.printf("  adult: %v %s\n2/2: step off, then step on again holding the baby\n", adult.Value, adult.Unit)
	if both, err = weighStable(ctx, a, e, adult.Time); err != nil {
		return adult, both, err
	}
	e.printf("  adult with baby: %v %s\n", both.Value, both.Unit)
	if both.Value < adult.Value {
		adult, both = both, adult
	}
	return adult, both, nil
}

// saveBaby stores the baby difference and the adult weight like BabyWeightViewModel.
func saveBaby(e *env, baby *store.User, adultName string, adult, both scale.Measurement) error {
	kg, at := app.BabyWeight(adult, both)
	if kg < 1 {
		text := app.FormatWeight(kg, both.Unit)
		ok, err := e.confirm("difference between 2 weigh-ins is " + text + "; is this the baby's weight?")
		if err != nil {
			return fmt.Errorf("baby weight %s is below 1 kg and needs a confirmation in a terminal", text)
		}
		if !ok {
			return nil
		}
	}
	e.data.Ignore(both.Time)
	r := store.Record{UserID: baby.ID, Time: at, Kind: store.KindBaby, Origin: store.OriginLive, WeightKg: kg, Unit: both.Unit}
	if err := app.Analyze(&r, *baby); err != nil {
		return err
	}
	r = e.data.Add(&r)
	saved := recordList{newRecordView(e.data, &r, both.Unit)}
	ar, err := saveBabyAdult(e, adultName, adult)
	if err != nil {
		return err
	}
	if ar != nil {
		saved = append(saved, newRecordView(e.data, ar, adult.Unit))
	}
	if err := e.save(); err != nil {
		return err
	}
	return e.render(saved)
}

// saveBabyAdult stores the adult weighing for --adult or the only match.
func saveBabyAdult(e *env, adultName string, adult scale.Measurement) (*store.Record, error) {
	var user *store.User
	if adultName != "" {
		u, err := e.data.User(adultName)
		if err != nil {
			return nil, err
		}
		user = u
	} else if matched := app.Match(e.data, adult.WeightKg()); len(matched) == 1 {
		user, _ = e.data.User(matched[0].ID)
	}
	if user == nil {
		return nil, nil
	}
	r, ok, err := app.SaveMeasurement(e.data, adult, user.ID, store.OriginLive, false)
	if err != nil || !ok {
		return nil, err
	}
	return &r, nil
}

func parseSex(s string) (bodycomp.Sex, error) {
	switch s {
	case "m":
		return bodycomp.Male, nil
	case "f":
		return bodycomp.Female, nil
	}
	return 0, errors.New("--sex must be m or f")
}
