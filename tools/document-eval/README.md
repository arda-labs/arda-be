# document-eval

Manual accuracy check for document-service on **fictional** CCCD cards
(three card versions, both sides, phone-photo distortions).

```bash
cd tools/document-eval
uv run gen_cards.py --out samples --per-version 6
# document-service running locally with a model configured:
uv run run_eval.py --base-url http://localhost:8100 --samples samples
```

Pass criteria (spec §3.5): id_number, date_of_birth and sex 100% correct;
all fields ≥ 90%. Exit code 1 otherwise. Run it after changing the model,
the prompt, or `cccd` normalization. Fonts: Arial on Windows, DejaVu Sans on
Linux (`--font-dir` to override).
