# How much of the body composition analysis can be trusted

This note evaluates the numbers the Mi Body Composition Scale 2 shows in Zepp Life 6.16.1, using the formulas recovered from the app (`libBodyfat.so` and the java layer, ported bit-exactly in `internal/bodycomp`). It covers what the scale measures, how much the measured impedance affects each value, how the other values are derived, and which of them carry real information.

The worked example throughout is a man of 39 years, 181 cm, 97.25 kg, impedance 421 Ohm.

## Summary

- The scale measures two quantities: weight and one foot-to-foot impedance value. Every other number is an estimate computed from weight, height, age, sex and that impedance; none of them is an independent measurement.
- Bioelectrical impedance analysis (BIA) as a method rests on solid physics, but the impedance has a small weight in these formulas. Over the whole range the formulas use (impedance is clamped to 200..1200 Ohm) body fat moves only between 29.3 % and 36.3 % for the example profile; if the impedance varies by tens of Ohm between days, body fat moves by tenths of a percent.
- The composition values are therefore mostly anthropometric estimates. For one person height, sex and age barely change, so their changes come from weight and, weakly, from impedance. Visceral fat and BMR do not use the impedance at all.
- No peer-reviewed validation of this model against a reference method was found, neither for single values nor for trends. The analysis of the formulas shows what drives each value; it does not prove how wrong the values are against real body composition.
- Weight is a direct measurement. BMI is a standard index computed from it. Body fat, water, bone mass, "muscle", protein, visceral fat and BMR are model estimates of unknown accuracy. Body age, body score and body type are app indices without an established clinical interpretation or validation.

## What the scale measures

The scale reports weight and a single impedance in Ohm, measured through four electrodes under the feet (`internal/scale/measurement.go`). The app computes composition only when the scale reports a stable impedance (`internal/app/records.go:73-77`). The native library accepts 50..3000 Ohm and clamps the value to 200..1200 Ohm before its formulas (`internal/bodycomp/native.go:28-40`); body age is computed in java from the raw value.

## The method and how this implementation departs from it

BIA estimates total body water and fat-free mass (FFM) from the fact that body water conducts current: by Ohm's law the conducting volume is proportional to H²/Z (height squared over impedance). Prediction equations are regressions of H²/Z, weight, age and sex against a reference method (DXA, hydrodensitometry, deuterium dilution) in a calibration population (Lukaski 1985, Kushner & Schoeller 1986, NIH 1996, Kyle 2004). Known limits even for good devices: equations are population specific, results depend on hydration, meals, exercise and time of day, and the sensitivity depends on electrode placement and body geometry. For conventional wrist-to-ankle BIA, Kyle et al. report that the trunk can account for about half of body mass but only about one tenth of the measured impedance (Kyle 2004, part I, after Foster & Lukaski 1996); these proportions should not be assumed for this foot-to-foot scale, although its current path also runs mostly through the legs.

The native library starts from an intermediate lean body mass coefficient (`native.go:43-49`):

```
LBM = 0.09058 * H² / 100 + 12.226 + 0.32 * W - 0.0068 * Z - 0.0542 * age
```

Body fat then uses LBM - 0.8 kg for men (68.04 kg and 67.24 kg in the example), so the coefficient is not exactly the fat-free mass shown later. Height and impedance enter separately, not as H²/Z, and the impedance coefficient is small: 100 Ohm change LBM by 0.68 kg. For comparison, in the Geneva equation (Kyle 2001) the term 0.518 * H²/R alone gives 40.3 kg of FFM for the example, and 100 Ohm (421 -> 521) shift it by 7.7 kg. The Geneva equation uses hand-to-foot resistance and reactance, a different calibration population and a different target, so this is a comparison of structure, not an accuracy check: in a published BIA equation the impedance term is the main predictor, here it is a minor correction.

## Influence of the impedance

Example profile, values of `bodycomp.Compute` rounded to two decimals. The app and the CLI truncate on screen, so for 421 Ohm they show 30.8 %, 49.3 %, 63.81 kg and 3.42 kg (`cmd/miscale/output.go:220-226`).

| Impedance, Ohm | Body fat, % | Water, % | Muscle, kg | Bone, kg | Visceral | BMR, kcal | Body age |
|---|---|---|---|---|---|---|---|
| 200 (clamp minimum) | 29.31 | 50.47 | 65.24 | 3.51 | 14 | 1846 | 34 |
| 321 | 30.15 | 49.87 | 64.46 | 3.46 | 14 | 1846 | 41 |
| 421 | 30.85 | 49.37 | 63.81 | 3.43 | 14 | 1846 | 46 |
| 521 | 31.55 | 48.87 | 63.17 | 3.39 | 14 | 1846 | 51 |
| 1200 (clamp maximum) | 36.30 | 45.48 | 58.79 | 3.16 | 14 | 1846 | 80 (clamped) |

- 100 Ohm change body fat by 0.70 percentage points; a change of tens of Ohm changes it by tenths.
- Visceral fat and BMR do not depend on the impedance.
- Body age changes by 5.17 "years" per 100 Ohm (male coefficient) before the clamp to 15..80 and the integer cast (`internal/bodycomp/bodycomp.go:120-135`). It takes the raw impedance, so it still changes below 200 Ohm, where the other values stay fixed (50 Ohm gives 26).

## Influence of the weight

With the impedance and the other inputs unchanged, 1 kg more weight (97.25 -> 98.25 kg) gives body fat 30.85 -> 31.23 %, fat mass +0.68 kg, fat-free mass +0.32 kg, water 49.37 -> 49.10 %, BMR 1846 -> 1861 kcal, body age 46 -> 47 (male branch above 61 kg, no clamp reached). Under these conditions the model books a weight change as 68 % fat and 32 % lean mass whatever its cause; it can tell otherwise only through the impedance, whose coefficient is small. Whether the body fat trend of this model follows the real fat trend would need a longitudinal validation, which was not found.

## How each value is computed

| Value | Formula in the app | Assessment |
|---|---|---|
| Weight | measured | reliable |
| BMI | W / H², clamped to 10..50 and truncated to 0.1 (`bodycomp.go:102-115`); levels at 18.5 / 25 / 30 (`standards.go:196-206`) | WHO cut-offs (WHO 2000); valid for populations, misleading for individuals with high muscle mass |
| Body fat, % | `(1 - (LBM - 0.8) / W) * 100` for men (times 0.98 below 61 kg); women subtract 9.25 up to 49 years or 7.25 above, with weight and height corrections; clamped to 5..75 (`native.go:52-89`) | anthropometric estimate with a small impedance term; level boundaries for men 18..39 are 11 / 17 / 22 / 27 % (`standards.go:37`), similar to but not the same as the preliminary BMI-derived ranges of Gallagher 2000 (men 20..39 in the African-American and white sample: BMI 18.5, 25 and 30 correspond to about 8, 20 and 25 % fat; other ranges for the Asian sample); the source of the app tables is not stated |
| Water, % | `(100 - fat) * 0.7`, times 0.98 above 50 or 1.02 otherwise, clamped to 35..75 (`native.go:92-100`) | not measured separately: a fixed transform of the fat estimate. The effective hydration of fat-free mass is 0.686 or 0.714, different from the classic assumption of about 0.73 (Pace & Rathbun 1945); higher body fat gives lower water by construction |
| Bone mass, kg | `LBM * 0.05158 - 0.18016894` for men or `- 0.245691014` for women, then +0.1 above 2.2 kg or -0.1 otherwise, clamped to 0.5..8 (`native.go:102-116`) | a fixed share of the LBM coefficient, not a measurement; BIA does not measure bone. DXA estimates bone mineral content and density, not the whole bone mass |
| Muscle, kg | `W - fat mass - bone`, clamped to 10..120 (`native.go:118-122`) | fat-free mass without bone, including organs and water, not skeletal muscle. The example gets 63.8 kg "high"; the skeletal muscle equation of Janssen 2000 would give about 37 kg (hand-to-foot resistance, illustrative) |
| Protein, % | `muscle / W * 100 - water`, clamped to 5..32 (`bodycomp.go:137-147`) | arithmetic of two derived values |
| Visceral fat | function of weight, height and age only, clamped to 1..50 (`native.go:124-150`) | no impedance at all; even where it is used, the trunk sensitivity of a foot-to-foot measurement is not established. Reference methods are CT or MRI; the waist-to-height ratio, with 0.5 as the threshold (NICE NG246, 2025, which replaced CG189), is a well-studied simple marker of central adiposity, used together with BMI; it is not a measurement of visceral fat either |
| BMR, kcal | men `877.8 + 14.916 W - 0.726 H - 8.976 age`, women `864.6 + 10.2036 W - 0.39336 H - 6.204 age`, clamped to 500..10000 and cast to an integer (`native.go:152-162`, `bodycomp.go:92`) | empirical regression without impedance; its negative height coefficient differs from the published equations, which by itself does not disprove it. The example gets 1846 kcal; Mifflin-St Jeor gives 1914 and revised Harris-Benedict 2038. Frankenfield 2005 rates equations by the share of predictions within ±10 % of measured values and reports large individual errors even for the best ones |
| BMR "standard" | `kcal/kg factor * W` cast to an integer, 21 kcal/kg for men 30..49 (`native.go:164-188`, `standards.go:190-193`) | proportional to total weight, while the BMR formula grows by 14.916 kcal per kg (men). For the example height, age and sex the two lines cross at 65.14 kg, so above that weight the app shows "below standard" by construction (example: 1846 against 2042). This is a comparison of two formulas, not a measured metabolic deficit. For context, adipose tissue spends about 4.5 kcal/kg/day against about 13 for skeletal muscle and much more for organs (Elia 1992; Wang 2010) |
| Ideal weight | men `(H - 80) * 0.7`, women `(H - 70) * 0.6` (`bodycomp.go:149-155`) | a Broca-type rule of thumb; 70.7 kg for 181 cm is BMI 21.6. The medical range is BMI 18.5..24.9, 60.6..81.6 kg for 181 cm |
| Body age | linear in height, weight, age and raw impedance, clamped to 15..80 (`bodycomp.go:118-135`) | app index without an established clinical interpretation or validation; the male coefficients are 0.92 "years" per kg and 5.17 per 100 Ohm before the clamp and the integer cast |
| Body type | grid of the fat level and the muscle level (`bodycomp.go:177-188`) | derived from two estimates; no established validation |
| Body score | 100 minus penalties for BMI, fat, muscle, water, visceral fat, bone and BMR (`score.go:154-168`) | app heuristic built on the estimates above; no established validation |

## Independent validation

A web search in October 2026 found no peer-reviewed validation of the XMTZC05HM; this is a limit of the search, not proof that no study exists. For a different, later model (S400) the manufacturer cites a Beijing Sport University report (BSU20220389) with a fat mass correlation of at least 0.93 against DXA. Correlation is not agreement: a device can be off by several kilograms of fat for every user and still correlate strongly; agreement needs a Bland-Altman analysis.

An independent comparison of three consumer smart scales with DXA (Frija-Masson 2021; Tefal, Terraillon and Nokia Withings, no Xiaomi) found median weight differences of 0..0.3 kg and median fat mass biases of -2.2..-4.4 kg per model; these are group medians, individual errors can be larger. The authors conclude that smart scales are not accurate for body composition. A study of 15 BIA devices against a four-compartment model (Br J Nutr 2023, no Xiaomi either) shows that reliability, cross-sectional validity and longitudinal validity differ between devices and have to be tested for each model.

## Practical use

- Weight and its trend: a direct measurement.
- BMI: a standard screening index, limited for individuals.
- Body fat percent and the other estimates: if used at all, compare readings taken under the same conditions (morning, fasting, barefoot) for reproducibility; this does not make the trend a validated fat trend, and in this model it largely follows the weight.
- Central adiposity: waist circumference or the waist-to-height ratio is a well-studied simple marker.
- Body composition with known accuracy: a reference method such as DXA. Segmental multi-frequency BIA devices with hand and foot electrodes are still estimates and need a validation of the specific model for the population.

## References

- Lukaski HC, Johnson PE, Bolonchuk WW, Lykken GI. Assessment of fat-free mass using bioelectrical impedance measurements of the human body. Am J Clin Nutr 1985;41(4):810-817. doi:10.1093/ajcn/41.4.810
- Kushner RF, Schoeller DA. Estimation of total body water by bioelectrical impedance analysis. Am J Clin Nutr 1986;44(3):417-424. doi:10.1093/ajcn/44.3.417
- NIH Technology Assessment Conference Statement. Bioelectrical impedance analysis in body composition measurement. Am J Clin Nutr 1996;64(3 Suppl):524S-532S. doi:10.1093/ajcn/64.3.524S
- Kyle UG, Genton L, Karsegard L, Slosman DO, Pichard C. Single prediction equation for bioelectrical impedance analysis in adults aged 20-94 years. Nutrition 2001;17(3):248-253. doi:10.1016/S0899-9007(00)00553-0
- Kyle UG et al. Bioelectrical impedance analysis, part I: review of principles and methods. Clin Nutr 2004;23(5):1226-1243. https://www.espen.org/documents/BIA1.pdf
- Kyle UG et al. Bioelectrical impedance analysis, part II: utilization in clinical practice. Clin Nutr 2004;23(6):1430-1453. https://www.espen.org/documents/BIA2.pdf
- Foster KR, Lukaski HC. Whole-body impedance: what does it measure? Am J Clin Nutr 1996;64(3 Suppl):388S-396S. doi:10.1093/ajcn/64.3.388S
- Gallagher D et al. Healthy percentage body fat ranges: an approach for developing guidelines based on body mass index. Am J Clin Nutr 2000;72(3):694-701. https://pubmed.ncbi.nlm.nih.gov/10966886/
- Janssen I, Heymsfield SB, Baumgartner RN, Ross R. Estimation of skeletal muscle mass by bioelectrical impedance analysis. J Appl Physiol 2000;89(2):465-471. doi:10.1152/jappl.2000.89.2.465
- Mifflin MD et al. A new predictive equation for resting energy expenditure in healthy individuals. Am J Clin Nutr 1990;51(2):241-247. doi:10.1093/ajcn/51.2.241
- Roza AM, Shizgal HM. The Harris Benedict equation reevaluated: resting energy requirements and the body cell mass. Am J Clin Nutr 1984;40(1):168-182. doi:10.1093/ajcn/40.1.168
- Frankenfield D, Roth-Yousey L, Compher C. Comparison of predictive equations for resting metabolic rate in healthy nonobese and obese adults: a systematic review. J Am Diet Assoc 2005;105(5):775-789. doi:10.1016/j.jada.2005.02.005
- Elia M. Organ and tissue contribution to metabolic rate. In: Kinney JM, Tucker HN (eds). Energy Metabolism: Tissue Determinants and Cellular Corollaries. Raven Press, New York, 1992, pp. 61-80.
- Wang Z et al. Specific metabolic rates of major organs and tissues across adulthood: evaluation by mechanistic model of resting energy expenditure. Am J Clin Nutr 2010;92(6):1369-1377. doi:10.3945/ajcn.2010.29885 (tests the values of Elia 1992 against whole-body resting energy expenditure with MRI organ masses).
- Pace N, Rathbun EN. Studies on body composition III. The body water and chemically combined nitrogen content in relation to fat content. J Biol Chem 1945;158(3):685-691. doi:10.1016/S0021-9258(19)51345-X
- WHO. Obesity: preventing and managing the global epidemic. Technical Report Series 894, 2000. https://pubmed.ncbi.nlm.nih.gov/11234459/
- NICE. Overweight and obesity management (NG246), 2025. https://www.nice.org.uk/guidance/ng246/chapter/Identifying-and-assessing-overweight-obesity-and-central-adiposity
- Frija-Masson J et al. Accuracy of smart scales on weight and body composition: observational study. JMIR Mhealth Uhealth 2021;9(4):e22487. https://mhealth.jmir.org/2021/4/e22487
- Assessing the reliability and cross-sectional and longitudinal validity of fifteen bioelectrical impedance analysis devices. Br J Nutr 2023. https://pubmed.ncbi.nlm.nih.gov/36404739/
- Xiaomi Body Composition Scale S400 specification (manufacturer-cited DXA correlation). https://www.mi.com/global/product/xiaomi-body-composition-scale-s400/
