import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { fileURLToPath } from "node:url"

/**
 * Golden-set drift gate.
 *
 * The canonical dev golden set lives in scripts/ai-dev-corpus/. The in-cluster
 * eval CronJob (arda-infra k8s/apps/ai-eval-cronjob.yaml) needs the same file
 * inside the ai-service image, and the Dockerfile build context is
 * apps/ai-service/, so a verified copy lives at
 * apps/ai-service/eval/evaluation-set.yaml.
 *
 * This check fails when the two diverge; update the copy whenever the
 * canonical set changes:
 *   Copy-Item scripts/ai-dev-corpus/evaluation-set.yaml apps/ai-service/eval/evaluation-set.yaml
 */

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const canonical = readFileSync(
  resolve(root, "scripts/ai-dev-corpus/evaluation-set.yaml"),
  "utf8"
)
const bundled = readFileSync(
  resolve(root, "apps/ai-service/eval/evaluation-set.yaml"),
  "utf8"
)

const normalize = (value) => value.replaceAll("\r\n", "\n").trimEnd()
if (normalize(canonical) !== normalize(bundled)) {
  console.error(
    [
      "AI eval set drift: apps/ai-service/eval/evaluation-set.yaml differs from scripts/ai-dev-corpus/evaluation-set.yaml",
      "Refresh the bundled copy so the in-cluster eval job runs the canonical golden set.",
    ].join("\n")
  )
  process.exit(1)
}

console.log("AI eval set copy in sync with the canonical dev golden set")
