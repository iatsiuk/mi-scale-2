package bodycomp

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"testing"
)

// The oracle CSVs come from executing the real libBodyfat.so under Unicorn
// (tools/bodyfat-oracle). Every native output must match bit for bit.
func TestNativeMatchesLibBodyfat(t *testing.T) {
	for _, name := range []string{"native_f64", "app_f32"} {
		t.Run(name, func(t *testing.T) {
			rows := readOracle(t, "testdata/"+name+".csv.gz")
			if len(rows) != 14222 {
				t.Fatalf("got %d rows, want 14222", len(rows))
			}
			failed := 0
			for _, r := range rows {
				if err := checkOracleRow(r); err != nil {
					failed++
					if failed <= 10 {
						t.Errorf("case %s: %v", r["case_id"], err)
					}
				}
			}
			if failed > 0 {
				t.Errorf("%d of %d rows differ", failed, len(rows))
			}
		})
	}
}

func readOracle(t *testing.T, path string) []map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	cr := csv.NewReader(gz)
	header, err := cr.Read()
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]string
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		row := make(map[string]string, len(header))
		for i, h := range header {
			row[h] = rec[i]
		}
		rows = append(rows, row)
	}
	return rows
}

func hex64(s string) (float64, error) {
	u, err := strconv.ParseUint(s, 0, 64)
	return math.Float64frombits(u), err
}

func checkOracleRow(r map[string]string) error {
	w, err := hex64(r["weight_hex64"])
	if err != nil {
		return err
	}
	h, err := hex64(r["height_hex64"])
	if err != nil {
		return err
	}
	age, _ := strconv.Atoi(r["age"])
	sex, _ := strconv.Atoi(r["sex"])
	z0, _ := strconv.Atoi(r["impedance_raw"])
	rc, _ := strconv.Atoi(r["nn_rc"])

	n, code := newNative(w, h, age, sex == 1, z0)
	if code != rc {
		return fmt.Errorf("nn code %d, want %d", code, rc)
	}
	if code != 0 {
		return nil
	}
	fat, bone := n.fat(), n.bone()
	got := map[string]float64{
		"AA":   float64(n.z),
		"BB":   n.bmr(),
		"CC":   fat,
		"DD":   bone,
		"FF":   n.muscle(fat, bone),
		"GG":   n.visceral(),
		"HH":   n.water(fat),
		"JJ_0": n.bmrStandard(),
	}
	for k, v := range got {
		want, err := hex64(r[k+"_hex64"])
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		if math.Float64bits(v) != math.Float64bits(want) {
			return fmt.Errorf("%s = %v (%#x), want %v (%s)", k, v, math.Float64bits(v), want, r[k+"_hex64"])
		}
	}
	return nil
}
