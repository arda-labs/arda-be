// Package arda-doc is the reusable document pipeline for Arda flows:
// template registry contract, template fill/render primitives, a Gotenberg
// conversion client, and snapshot helpers that persist generated artifacts
// into media-service as attachments of a business entity (e.g. a workflow
// business case).
//
// Responsibilities are split so domain services keep data + rules and never
// touch object storage directly:
//
//   - template.go  — DocTemplate registry contract (rows live in domain config
//     tables; layout blobs live in media-service)
//   - fill.go      — xlsx template fill via defined names / explicit refs
//   - html.go      — Go html/template rendering for print-oriented documents
//   - gotenberg.go — HTML→PDF and Office→PDF conversion (Gotenberg service)
//   - media.go     — multipart upload into media-service with tenant headers
//   - snapshot.go  — render → convert → upload orchestration
package ardadoc
