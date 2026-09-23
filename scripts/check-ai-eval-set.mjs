import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { fileURLToPath } from "node:url"

/**
 * Golden-set drift gate.
 *
 * Canonical dev golden sets live in scripts/ai-dev-corpus/. The in-cluster eval
 * CronJob (arda-infra k8s/apps/ai-eval-cronjob.yaml) needs the same files inside
 * the ai-service image, and the Dockerfile build context is apps/ai-service/, so
 * verified copies live in apps/ai-service/eval/.
 *
 * This check fails when a canonical set and its bundled copy diverge; update the
 * copy whenever the canonical set changes:
 *   Copy-Item scripts/ai-dev-corpus/evaluation-set.yaml apps/ai-service/eval/evaluation-set.yaml
 *   Copy-Item scripts/ai-dev-corpus/routing-evaluation-set.yaml apps/ai-service/eval/routing-evaluation-set.yaml
 */

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const pairs = [
  {
    canonical: "scripts/ai-dev-corpus/evaluation-set.yaml",
    bundled: "apps/ai-service/eval/evaluation-set.yaml",
  },
  {
    canonical: "scripts/ai-dev-corpus/routing-evaluation-set.yaml",
    bundled: "apps/ai-service/eval/routing-evaluation-set.yaml",
  },
]

const normalize = (value) => value.replaceAll("\r\n", "\n").trimEnd()
const failures = []
for (const pair of pairs) {
  const canonical = readFileSync(resolve(root, pair.canonical), "utf8")
  const bundled = readFileSync(resolve(root, pair.bundled), "utf8")
  if (normalize(canonical) !== normalize(bundled)) {
    failures.push(
      [
        `AI eval set drift: ${pair.bundled} differs from ${pair.canonical}`,
        "Refresh the bundled copy so the in-cluster eval job runs the canonical golden set.",
      ].join("\n")
    )
  }
}
if (failures.length > 0) {
  console.error(failures.join("\n\n"))
  process.exit(1)
}
console.log(`AI eval set copies in sync with the canonical dev golden sets (${pairs.length})`)
