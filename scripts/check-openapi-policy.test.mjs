import test from "node:test"
import assert from "node:assert/strict"
import { comparePolicy, covers, expandOperations, overlaps, parsePolicyRoutes } from "./check-openapi-policy.mjs"

const policy = `
routes:
  - id: x-public
    path: /api/x/public/branding
    methods: [GET]
    auth: false
    risk: public

  - id: x-read
    path: /api/x/**
    methods: [GET]
    auth: true
    risk: low
    permissions:
      - x.read

  - id: x-write
    path: /api/x/**
    methods: [POST, PUT, PATCH, DELETE, OPTIONS]
    auth: true

  - id: x-catalog-currencies
    path: /api/x/catalogs/currencies/*
    auth: true

  - id: other-read
    path: /api/other/**
    methods: [GET]
    auth: true
`

function document(paths) {
  const out = { paths: {} }
  for (const [route, params] of Object.entries(paths)) {
    const [method, path] = route.split(" ")
    out.paths[path] = { ...(out.paths[path] ?? {}), [method.toLowerCase()]: { operationId: route, parameters: params ?? [] } }
  }
  return out
}

test("parses policy routes with methods and auth", () => {
  const routes = parsePolicyRoutes(policy)
  assert.deepEqual(routes.map((r) => [r.id, r.path, r.methods, r.auth]), [
    ["x-public", "/api/x/public/branding", ["GET"], false],
    ["x-read", "/api/x/**", ["GET"], true],
    ["x-write", "/api/x/**", ["POST", "PUT", "PATCH", "DELETE", "OPTIONS"], true],
    ["x-catalog-currencies", "/api/x/catalogs/currencies/*", [], true],
    ["other-read", "/api/other/**", ["GET"], true],
  ])
})

test("matches paths the way the gateway does", () => {
  assert.ok(overlaps("/api/x/**", "/api/x"))
  assert.ok(overlaps("/api/x/**", "/api/x/items/{id}"))
  assert.ok(overlaps("/api/x/items/*", "/api/x/items/{id}/sub"))
  assert.ok(overlaps("/api/x/items/{id}", "/api/x/items/{id}"))
  assert.ok(overlaps("/api/x/items/abc", "/api/x/items/{id}"), "a literal overlaps a param for one value")
  assert.ok(!overlaps("/api/x/items", "/api/x/items/{id}"))
  assert.ok(!overlaps("/api/x/other/**", "/api/x/items"))
  assert.ok(covers("/api/x/**", "/api/x/items/{id}"))
  assert.ok(covers("/api/x/items/", "/api/x/items"), "trailing slash is ignored")
  assert.ok(!covers("/api/x/items/abc", "/api/x/items/{id}"), "a literal does not cover every value of a param")
  assert.ok(!covers("/api/x/items/{id}", "/api/x/items/{id}"), "the gateway has no params; braces are literal")
})

test("expands enum path parameters", () => {
  const doc = document({ "GET /api/x/catalogs/{catalog}/{id}": [{ name: "catalog", in: "path", schema: { enum: ["currencies", "countries"] } }] })
  assert.deepEqual(expandOperations(doc).map((op) => op.path), ["/api/x/catalogs/currencies/{id}", "/api/x/catalogs/countries/{id}"])
})

test("a consistent policy and document pass", () => {
  const routes = parsePolicyRoutes(policy)
  const doc = document({
    "GET /api/x/public/branding": [],
    "GET /api/x/items": [],
    "POST /api/x/items": [],
    "DELETE /api/x/items/{id}": [],
    "GET /api/x/catalogs/{catalog}/{id}": [{ name: "catalog", in: "path", schema: { enum: ["currencies"] } }],
  })
  assert.deepEqual(comparePolicy("x", routes, doc), [])
})

test("a policy route that matches nothing fails", () => {
  const routes = parsePolicyRoutes(policy)
  const doc = document({ "GET /api/x/items": [], "POST /api/x/items": [] })
  assert.deepEqual(comparePolicy("x", routes, doc), [
    "x: policy route x-public (GET /api/x/public/branding) matches no documented operation",
    "x: policy route x-catalog-currencies (ALL /api/x/catalogs/currencies/*) matches no documented operation",
  ])
})

test("an operation no policy route covers fails", () => {
  const routes = parsePolicyRoutes(`
routes:
  - id: x-read
    path: /api/x/items
    methods: [GET]
    auth: true
  - id: x-one
    path: /api/x/items/abc
    methods: [DELETE]
    auth: true
`)
  const doc = document({ "GET /api/x/items": [], "POST /api/x/items": [], "DELETE /api/x/items/{id}": [] })
  assert.deepEqual(comparePolicy("x", routes, doc), [
    "x: POST /api/x/items (POST /api/x/items) is covered by no policy route, the gateway never forwards it",
    "x: DELETE /api/x/items/{id} (DELETE /api/x/items/{id}) is covered by no policy route, the gateway never forwards it",
  ])
})

test("routes of other services are ignored", () => {
  const routes = parsePolicyRoutes(policy)
  const doc = document({ "GET /api/x/items": [], "POST /api/x/items": [], "GET /api/x/public/branding": [], "GET /api/x/catalogs/currencies": [] })
  assert.deepEqual(comparePolicy("x", routes, doc), [])
})
