# miscale

macOS command-line client for the Xiaomi Mi Body Composition Scale 2 (XMTZC05HM, also the first Body Composition Scale XMTZC02HM). It reproduces the scale features of Zepp Life 6.16.1 locally, without the Xiaomi cloud: live weighing, body composition analysis bit-exact with the app, offline history sync, family members, guest, infant and balance modes, and scale settings.

## Installation

Requires macOS, Go 1.25+ and Xcode Command Line Tools (CoreBluetooth needs cgo).

```bash
make build    # lint + build ./miscale with the embedded Bluetooth usage description
make install  # go install with the same flags
```

On first use macOS asks to allow Bluetooth for the terminal application.

## Quick Usage

```bash
# family members
miscale user add --name me --sex f --birth 2000-01 --height 160 --weight 50
miscale user list

# register the history key once (step on the scale to wake it up); the scale
# keeps offline records only for keys it has already seen
miscale sync

# weigh: step on the scale barefoot and wait until the analysis is done
miscale measure

# download measurements the scale stored while miscale was not running
miscale sync

# records
miscale history
miscale show 3
miscale -f json show 3

# scale settings and info (step on the scale to wake it up)
miscale scale info
miscale scale unit kg
miscale scale small-object off
```

## Commands

| Command                                                                        | Description                                                                                                                                                   |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `measure [--user NAME]`                                                        | Weigh with body composition; the user is matched by weight (< 3 kg from the last record) or asked; without a terminal an unmatched record is saved unassigned |
| `guest --sex m\|f --height CM --birth YYYY-MM`                                 | Analyze a guest without saving                                                                                                                                |
| `baby --baby NAME [--adult NAME]`                                              | Infant weight: adult alone, then holding the baby                                                                                                             |
| `balance [--user NAME]`                                                        | One-foot balance test with level 1..5                                                                                                                         |
| `sync [--no-ack]`                                                              | Download offline records; the scale deletes them after they are saved unless `--no-ack`                                                                       |
| `scale info`                                                                   | Model, firmware, serial; syncs the scale clock to UTC                                                                                                         |
| `scale unit kg\|lb\|jin`                                                       | Unit on the scale display                                                                                                                                     |
| `scale small-object on\|off`                                                   | Weighing of small objects                                                                                                                                     |
| `scale erase --yes`                                                            | Clear all data stored in the scale                                                                                                                            |
| `user add\|edit\|list\|rm`                                                     | Family members                                                                                                                                                |
| `add --user NAME --weight KG [--time RFC3339]`                                 | Manual weight record                                                                                                                                          |
| `history [--user NAME] [-n N]`                                                 | Records, newest first; `--user ''` lists unassigned ones                                                                                                      |
| `show ID`, `assign ID NAME`, `rm ID`                                           | Record details, assignment, deletion                                                                                                                          |
| `config [--merge on\|off] [--unit kg\|lb\|jin\|st] [--uid N] [--forget-scale]` | Settings                                                                                                                                                      |
| `scan`, `gatt`                                                                 | Raw advertisements and GATT dump for debugging                                                                                                                |

## Output

Results go to stdout as markdown tables (default) or JSON with `-f json`; progress and prompts go to stderr. `scan` streams one table row or one JSON object per changed advertisement.

## Data

Profiles, records and settings are stored in `~/Library/Application Support/miscale/data.json` (override with `--data` or `MISCALE_DATA`).

The scale keeps offline records per 32-bit history key and starts keeping them for a key at its first `sync`; weighings made before that are not available under the new key. miscale uses a random key; to fetch records the scale stored for a Zepp Life account set `miscale config --uid <low 32 bits of the account id>`.

## Accuracy

The scale measures weight and one foot-to-foot impedance; all body composition values are estimates computed from them and from height, age and sex, with a small weight of the impedance, and no validation of this model was found. See [How much of the body composition analysis can be trusted](docs/body-composition-validity.md).

## Not supported

- Firmware update (Huami DFU is not analyzed; a failed update could brick the scale)
- Chinese locale standards (BMI tables for children, jin in the unit list of the app)
- Xiaomi cloud sync
