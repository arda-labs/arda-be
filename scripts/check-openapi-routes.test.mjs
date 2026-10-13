import test from "node:test"
import assert from "node:assert/strict"
import { compareRoutes, documentRoutes, routerRoutes } from "./check-openapi-routes.mjs"

const handlers = new Map([
  ["CaseByID", "func (h *H) CaseByID(w http.ResponseWriter, r *http.Request) {\n\tswitch {\n\tcase r.Method == http.MethodGet:\n\tcase r.Method == http.MethodPost && action == \"submit\":\n\t}\n}\n"],
  ["Pause", "func (h *H) Pause(w http.ResponseWriter, r *http.Request) {\n\tif r.Method != http.MethodPost {\n\t}\n}\n"],
  ["Detail", "func (h *H) Detail(w http.ResponseWriter, r *http.Request) {\n\tif r.Method != http.MethodGet {\n\t}\n}\n"],
  ["Items", "func (h *H) Items(w http.ResponseWriter, r *http.Request) {\n\tif r.Method != http.MethodGet {\n\t}\n}\n"],
])

const router = `
	mux.HandleFunc("/api/x/items", h.Items)
	mux.HandleFunc("/api/x/cases/", h.CaseByID)
	mux.HandleFunc("/api/x/operate/instances/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/pause"):
			h.Pause(w, r)
		default:
			h.Detail(w, r)
		}
	})
	for _, kind := range kinds {
		base := "/api/x/adjust/" + kind
		mux.HandleFunc(base+"/{id}", h.Items)
	}
`

function doc(paths) {
  const out = {}
  for (const route of paths) {
    const [method, path] = route.split(" ")
    out[path] = { ...(out[path] ?? {}), [method.toLowerCase()]: { operationId: route } }
  }
  return { paths: out }
}

test("reads subtree routes with the methods of the closure and the handlers it calls", () => {
  const served = routerRoutes(router, handlers)
  assert.deepEqual([...served.routes].sort(), ["GET /api/x/adjust/{kind}/{id}", "GET /api/x/items"])
  assert.deepEqual(
    [...served.subtrees].map(([prefix, methods]) => [prefix, [...methods].sort()]),
    [
      ["/api/x/cases/", ["GET", "POST"]],
      ["/api/x/operate/instances/", ["GET", "POST"]],
    ],
  )
  assert.deepEqual(served.problems, [])
})

const complete = [
  "GET /api/x/items",
  "GET /api/x/adjust/{kind}/{id}",
  "GET /api/x/cases/{id}",
  "POST /api/x/cases/{id}/submit",
  "GET /api/x/operate/instances/{key}",
  "POST /api/x/operate/instances/{key}/pause",
]

test("accepts a document that covers every route and subtree method", () => {
  const served = routerRoutes(router, handlers)
  assert.deepEqual(compareRoutes("x", "x.json", served, documentRoutes(doc(complete))), [])
})

test("fails when a subtree method has no operation under it", () => {
  const served = routerRoutes(router, handlers)
  const failures = compareRoutes("x", "x.json", served, documentRoutes(doc(complete.filter((r) => !r.startsWith("POST /api/x/cases/")))))
  assert.deepEqual(failures, ["x: POST /api/x/cases/* is served but no operation under it is documented in x.json"])
})

test("fails when an operation is outside every route and subtree", () => {
  const served = routerRoutes(router, handlers)
  const failures = compareRoutes("x", "x.json", served, documentRoutes(doc([...complete, "DELETE /api/x/cases/{id}", "GET /api/x/other"])))
  assert.deepEqual(failures, [
    "x: DELETE /api/x/cases/{id} is documented in x.json but not served",
    "x: GET /api/x/other is documented in x.json but not served",
  ])
})

test("fails when an exact route is missing from the document", () => {
  const served = routerRoutes(router, handlers)
  const failures = compareRoutes("x", "x.json", served, documentRoutes(doc(complete.filter((r) => r !== "GET /api/x/items"))))
  assert.deepEqual(failures, ["x: GET /api/x/items is served but missing from x.json"])
})

test("reports a subtree whose methods cannot be read", () => {
  const served = routerRoutes('mux.HandleFunc("/api/x/opaque/", h.Unknown)\n', new Map())
  assert.deepEqual(served.problems, ["cannot tell which methods /api/x/opaque/ serves"])
})
