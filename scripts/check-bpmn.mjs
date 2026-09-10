import { readFile, readdir } from "node:fs/promises"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

// BPMN structural gate for the workflow-service built-in process corpus.
// Guards invariants that Zeebe accepts silently but that break the runtime or
// the approval semantics: dangling refs (the Flow_Start_Maker class of bug),
// conditions on default flows, decision flows without conditions, duplicate
// ids, unreachable nodes, and the standardized `decision` review variable.

const root = resolve(fileURLToPath(new URL("..", import.meta.url)))
const bpmnDir = join(root, "apps", "workflow-service", "internal", "bootstrap")

const nodeTags = [
  "startEvent",
  "endEvent",
  "userTask",
  "serviceTask",
  "sendTask",
  "receiveTask",
  "exclusiveGateway",
  "parallelGateway",
  "inclusiveGateway",
  "boundaryEvent",
  "intermediateCatchEvent",
  "intermediateThrowEvent",
  "subProcess",
  "callActivity",
]

const errors = []
const files = (await readdir(bpmnDir)).filter((f) => f.endsWith(".bpmn")).sort()

function attr(source, name) {
  const match = source.match(new RegExp(`${name}="([^"]*)"`))
  return match ? match[1] : null
}

for (const file of files) {
  const xml = await readFile(join(bpmnDir, file), "utf8")
  const fail = (message) => errors.push(`${file}: ${message}`)

  const processMatch = xml.match(/<bpmn:process[^>]*id="([^"]+)"[\s\S]*?<\/bpmn:process>/)
  if (!processMatch) {
    fail("no bpmn:process element")
    continue
  }
  const processId = attr(processMatch[0], "id")
  const process = processMatch[0]
  if (`${processId}.bpmn` !== file) fail(`process id "${processId}" does not match filename`)

  const nodes = []
  for (const tag of nodeTags) {
    const open = new RegExp(`<bpmn:${tag}\\b[^>]*>`, "g")
    let match
    while ((match = open.exec(process))) {
      let block = match[0]
      if (!match[0].endsWith("/>")) {
        const close = `</bpmn:${tag}>`
        const end = process.indexOf(close, match.index)
        block = process.slice(match.index, end + close.length)
      }
      nodes.push({ tag, id: attr(match[0], "id"), block })
    }
  }

  const flows = []
  const flowPattern = /<bpmn:sequenceFlow\b[\s\S]*?(?:\/>|<\/bpmn:sequenceFlow>)/g
  let flowMatch
  while ((flowMatch = flowPattern.exec(process))) {
    const source = flowMatch[0]
    flows.push({
      id: attr(source, "id"),
      source: attr(source, "sourceRef"),
      target: attr(source, "targetRef"),
      hasCondition: /<bpmn:conditionExpression/.test(source),
      condition: (source.match(/<bpmn:conditionExpression[^>]*>([\s\S]*?)<\/bpmn:conditionExpression>/) || [])[1] || "",
    })
  }

  const nodeIds = new Set()
  for (const node of nodes) {
    if (nodeIds.has(node.id)) fail(`duplicate node id ${node.id}`)
    nodeIds.add(node.id)
  }

  const flowIds = new Set()
  for (const flow of flows) {
    if (flowIds.has(flow.id)) fail(`duplicate sequence flow id ${flow.id}`)
    flowIds.add(flow.id)
    if (!nodeIds.has(flow.source)) fail(`flow ${flow.id} sourceRef missing: ${flow.source}`)
    if (!nodeIds.has(flow.target)) fail(`flow ${flow.id} targetRef missing: ${flow.target}`)
  }

  const outgoing = new Map()
  const incoming = new Map()
  for (const flow of flows) {
    outgoing.set(flow.source, [...(outgoing.get(flow.source) || []), flow])
    incoming.set(flow.target, [...(incoming.get(flow.target) || []), flow])
  }

  for (const node of nodes) {
    const out = outgoing.get(node.id) || []
    const inc = incoming.get(node.id) || []
    if (node.tag === "startEvent" && out.length !== 1) fail(`start ${node.id} has ${out.length} outgoing flows`)
    if (node.tag === "endEvent" && out.length !== 0) fail(`end ${node.id} has outgoing flows`)
    if (node.tag === "endEvent" && inc.length === 0) fail(`end ${node.id} has no incoming flow`)

    const refOut = [...node.block.matchAll(/<bpmn:outgoing>([^<]+)<\/bpmn:outgoing>/g)].map((m) => m[1])
    const refIn = [...node.block.matchAll(/<bpmn:incoming>([^<]+)<\/bpmn:incoming>/g)].map((m) => m[1])
    for (const ref of refOut) if (!flowIds.has(ref)) fail(`${node.id} outgoing ref dangling: ${ref}`)
    for (const ref of refIn) if (!flowIds.has(ref)) fail(`${node.id} incoming ref dangling: ${ref}`)
    for (const flow of out) if (!refOut.includes(flow.id)) fail(`${node.id} missing <outgoing> for ${flow.id}`)
    for (const flow of inc) if (!refIn.includes(flow.id)) fail(`${node.id} missing <incoming> for ${flow.id}`)

    if (node.tag.endsWith("Gateway")) {
      const isExclusive = node.tag === "exclusiveGateway"
      const isParallel = node.tag === "parallelGateway"
      const defaultFlow = attr(node.block, "default")
      if (out.length > 1 && isExclusive && !defaultFlow) fail(`exclusive gateway ${node.id} has no default flow`)
      for (const flow of out) {
        if (flow.id === defaultFlow) {
          if (flow.hasCondition) fail(`gateway ${node.id} default flow ${flow.id} must not have a condition`)
        } else if (isExclusive && !flow.hasCondition) {
          fail(`exclusive gateway ${node.id} flow ${flow.id} lacks condition and is not default`)
        }
        if (isParallel && flow.hasCondition) fail(`parallel gateway ${node.id} flow ${flow.id} must not have a condition`)
      }
    } else if (!node.tag.endsWith("Event") && out.length > 1) {
      const defaultFlow = attr(node.block, "default")
      fail(`task ${node.id} has ${out.length} outgoing flows — branch through an exclusive gateway instead`)
      for (const flow of out) {
        if (flow.id === defaultFlow && flow.hasCondition) fail(`task ${node.id} default flow ${flow.id} has a condition`)
      }
    }

    if (node.tag === "boundaryEvent") {
      const attached = attr(node.block, "attachedToRef")
      if (!attached || !nodeIds.has(attached)) fail(`boundary ${node.id} attachedToRef missing: ${attached}`)
      if (!/EventDefinition/.test(node.block)) fail(`boundary ${node.id} lacks an event definition`)
    }
    if (node.tag === "serviceTask") {
      const type = (node.block.match(/<zeebe:taskDefinition[^>]*type="([^"]+)"/) || [])[1]
      if (!type) fail(`serviceTask ${node.id} missing zeebe:taskDefinition type`)
    }
    if (node.tag === "userTask") {
      if (!/zeebe:userTask/.test(node.block)) fail(`userTask ${node.id} missing zeebe:userTask`)
      if (!/zeebe:assignmentDefinition/.test(node.block)) fail(`userTask ${node.id} missing assignmentDefinition`)
      const groups = (node.block.match(/<zeebe:assignmentDefinition[^>]*candidateGroups="([^"]*)"/) || [])[1]
      if (!groups || !groups.trim()) fail(`userTask ${node.id} has empty candidateGroups`)
    }
  }

  for (const flow of flows) {
    if (flow.condition.includes("reviewDecision")) {
      fail(`flow ${flow.id} uses legacy reviewDecision — standardize on the decision variable`)
    }
  }

  const reachable = new Set()
  const start = nodes.find((node) => node.tag === "startEvent")
  if (start) {
    const stack = [start.id]
    while (stack.length) {
      const id = stack.pop()
      if (reachable.has(id)) continue
      reachable.add(id)
      for (const flow of outgoing.get(id) || []) stack.push(flow.target)
    }
  } else {
    fail("no startEvent")
  }
  for (const node of nodes) {
    if (node.tag === "boundaryEvent") continue
    if (!reachable.has(node.id)) fail(`node ${node.id} (${node.tag}) unreachable from start`)
  }
}

if (errors.length) {
  console.error(errors.join("\n"))
  process.exit(1)
}

console.log(`BPMN invariant OK: ${files.length} built-in processes checked`)
