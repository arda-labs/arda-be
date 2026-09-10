import { createHmac, randomBytes } from "node:crypto";
const BASE = process.env.BASE ?? "http://127.0.0.1:18081";
const SECRET = process.env.SECRET;
const TENANT = "00000000-0000-0000-0000-000000000010";
function signToken() {
  const now = Math.floor(Date.now()/1000);
  const c = {v:"v1",src:"auth-gateway",aud:"ai-service",iat:now,exp:now+120,nonce:randomBytes(16).toString("base64url")};
  const p = Buffer.from(JSON.stringify(c)).toString("base64url");
  return `v1.${p}.${createHmac("sha256",SECRET).update("v1."+p).digest("base64url")}`;
}
async function rag(query) {
  const res = await fetch(BASE+"/api/rag/query",{method:"POST",headers:{"Content-Type":"application/json","x-service-auth":signToken(),"X-Auth-Checked":"true","X-User-Id":"dev-tester","X-Tenant-Id":TENANT},body:JSON.stringify({query})});
  const t = await res.text();
  if (!res.ok) { console.error(query, "->", res.status, t.slice(0,200)); return; }
  const j = JSON.parse(t);
  console.log(`Q: ${query}`);
  console.log(`   hits=${j.hits?.length ?? j.results?.length ?? 0} keys=${Object.keys(j).join(",")}`);
  const hits = j.hits ?? j.results ?? [];
  for (const h of hits.slice(0,3)) console.log(`   - [${h.score ?? h.similarity ?? "?"}] ${(h.title ?? h.source_title ?? h.heading ?? h.id).toString().slice(0,60)} :: ${(h.content ?? h.text ?? "").toString().slice(0,80)}...`);
}
await rag("Nghỉ phép năm được bao nhiêu ngày?");
await rag("Hạn mức thanh toán cần Giám đốc Tài chính duyệt là bao nhiêu?");
await rag("Làm sao xử lý khi có sự cố Sev1?");
await rag("quy trình xuất dữ liệu khách hàng");
