import test from "node:test"
import assert from "node:assert/strict"
import { applyAllowlist, compareDocuments } from "./check-openapi-breaking.mjs"

const item = (props, required = Object.keys(props)) => ({ type: "object", required, properties: props })

function doc(overrides = () => {}) {
  const base = {
    paths: {
      "/api/x/items": {
        get: {
          parameters: [{ name: "status", in: "query", required: false, schema: { type: "string", enum: ["OPEN", "CLOSED"] } }],
          responses: { 200: { content: { "application/json": { schema: { $ref: "#/components/schemas/ItemSuccess" } } } } },
        },
        post: {
          requestBody: { required: true, content: { "application/json": { schema: { $ref: "#/components/schemas/ItemInput" } } } },
          responses: { 201: { content: { "application/json": { schema: { $ref: "#/components/schemas/ItemSuccess" } } } } },
        },
      },
      "/api/x/items/{id}": {
        delete: { responses: { 204: { description: "No Content" } } },
      },
    },
    components: {
      schemas: {
        Item: item({ id: { type: "string" }, amount: { type: "integer" }, note: { type: ["string", "null"] }, kind: { type: "string", enum: ["A", "B"] }, tags: { anyOf: [{ type: "array", items: { type: "string" } }, { type: "null" }] } }),
        ItemInput: item({ amount: { type: "integer" }, kind: { type: "string", enum: ["A", "B"] }, note: { type: ["string", "null"] } }, []),
        ItemSuccess: item({ result: { $ref: "#/components/schemas/Item" }, success: { const: true } }),
      },
    },
  }
  const copy = structuredClone(base)
  overrides(copy)
  return copy
}

test("identical documents and compatible changes pass", () => {
  assert.deepEqual(compareDocuments(doc(), doc()), [])
  const compatible = doc((d) => {
    d.paths["/api/x/items"].get.parameters.push({ name: "q", in: "query", required: false, schema: { type: "string" } })
    d.paths["/api/x/other"] = { get: { responses: { 200: {} } } }
    d.components.schemas.Item.properties.extra = { type: "string" }
    d.components.schemas.ItemInput.properties.amount = { type: ["integer", "string"] }
    d.components.schemas.Item.properties.note = { type: "string" }
    d.components.schemas.Renamed = d.components.schemas.Item
    delete d.components.schemas.Item
    d.components.schemas.ItemSuccess.properties.result = { $ref: "#/components/schemas/Renamed" }
  })
  assert.deepEqual(compareDocuments(doc(), compatible), [])
})

test("removed paths and operations", () => {
  const changed = doc((d) => {
    delete d.paths["/api/x/items/{id}"]
    delete d.paths["/api/x/items"].post
  })
  assert.deepEqual(compareDocuments(doc(), changed), ["operation POST /api/x/items removed", "path /api/x/items/{id} removed"])
})

test("response field removed, type widened and enum narrowed", () => {
  const changed = doc((d) => {
    delete d.components.schemas.Item.properties.amount
    d.components.schemas.Item.properties.id = { type: ["string", "null"] }
    d.components.schemas.Item.properties.kind = { type: "string", enum: ["A"] }
    d.components.schemas.Item.properties.tags = { anyOf: [{ type: "array", items: { type: "integer" } }, { type: "null" }] }
  })
  const changes = compareDocuments(doc(), changed).filter((c) => c.startsWith("GET"))
  assert.deepEqual(changes, [
    "GET /api/x/items: response 200 field result.amount removed",
    "GET /api/x/items: response 200 field result.id type string changed to null|string",
    "GET /api/x/items: response 200 field result.kind enum lost values B",
    "GET /api/x/items: response 200 field result.tags[] type string changed to integer",
  ])
})

test("request field newly required, type narrowed, enum narrowed, parameter required", () => {
  const changed = doc((d) => {
    d.components.schemas.ItemInput.required = ["amount"]
    d.components.schemas.ItemInput.properties.note = { type: "string" }
    d.components.schemas.ItemInput.properties.kind = { type: "string", enum: ["B"] }
    d.paths["/api/x/items"].get.parameters[0].required = true
    d.paths["/api/x/items"].get.parameters[0].schema.enum = ["OPEN"]
  })
  assert.deepEqual(compareDocuments(doc(), changed), [
    "GET /api/x/items: parameter status (query) is now required",
    "GET /api/x/items: parameter status (query) enum lost values CLOSED",
    "POST /api/x/items: request field kind enum lost values A",
    "POST /api/x/items: request field note type null|string changed to string",
    "POST /api/x/items: request field amount is now required",
  ])
})

test("changed success status, lost content types and newly required body", () => {
  const changed = doc((d) => {
    d.paths["/api/x/items"].post.responses = { 200: d.paths["/api/x/items"].post.responses[201] }
    d.paths["/api/x/items"].post.requestBody.content = { "multipart/form-data": { schema: { type: "object" } } }
    d.paths["/api/x/items"].get.responses[200].content = { "text/plain": { schema: { type: "string" } } }
    d.paths["/api/x/items/{id}"].delete.requestBody = { required: true, content: { "application/json": { schema: { type: "object" } } } }
  })
  assert.deepEqual(compareDocuments(doc(), changed), [
    "GET /api/x/items: response 200 lost content type application/json",
    "POST /api/x/items: request body content type application/json removed",
    "POST /api/x/items: success status 201 changed to 200",
    "DELETE /api/x/items/{id}: request body is now required",
  ])
})

test("the allowlist suppresses exact, unexpired entries only", () => {
  const changes = ["path /api/x/old removed", "GET /api/x/items: response 200 field result.amount removed"]
  const entries = [
    { document: "x-v1.json", change: "path /api/x/old removed", reason: "retired with the old screen", expires: "2026-12-31" },
    { document: "x-v1.json", change: "GET /api/x/items: response 200 field result.amount removed", reason: "expired", expires: "2026-01-01" },
    { document: "y-v1.json", change: "GET /api/x/items: response 200 field result.amount removed", reason: "other document", expires: "2026-12-31" },
  ]
  const { failures, used } = applyAllowlist("x-v1.json", changes, entries, "2026-10-13")
  assert.deepEqual(failures, ["GET /api/x/items: response 200 field result.amount removed"])
  assert.deepEqual(used, [entries[0]])
})
