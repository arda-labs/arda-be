# document-eval

Manual accuracy check for document-service on **fictional** CCCD cards
(three card versions, both sides, phone-photo distortions).

```bash
cd tools/document-eval
uv run gen_cards.py --out samples --per-version 6
# document-service running locally with a model configured:
uv run run_eval.py --base-url http://localhost:8100 --samples samples
```

Pass criteria (spec §3.5): id_number, all dates (date_of_birth, issue_date,
expiry_date) and sex 100% correct; every field individually ≥ 90%. Exit
code 1 otherwise. Run it after changing the model,
the prompt, or `cccd` normalization. Fonts: Arial on Windows, DejaVu Sans on
Linux (`--font-dir` to override).

Generated ID numbers use real province codes with random digits and may coincide with real numbers; the cards themselves are fictional.
