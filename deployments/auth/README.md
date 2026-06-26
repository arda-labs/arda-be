# Arda Auth Stack

Thư mục này chứa cấu hình deploy Hydra + Kratos bằng Helm.

## Cấu trúc

```text
auth/
├── setup.sh
├── values-hydra.yaml
├── values-kratos.yaml
├── identity.schema.json
└── clients/
    └── arda-shell.json
```

## Cài đặt

```bash
cd auth
chmod +x setup.sh
./setup.sh
```

## Vai trò

```text
Hydra  = OAuth2/OIDC, authorization code, token, OAuth client
Kratos = identity, login, password, session, device/session management
IAM    = RBAC/permission/menu/org tự xây sau
```

## Flow mong muốn

```text
App
→ Hydra /oauth2/auth
→ /login?login_challenge=...
→ Login app dùng Kratos để xác thực user
→ Login app accept Hydra login challenge
→ /consent?consent_challenge=...
→ auto accept consent
→ /callback?code=...
→ Dashboard
```

## Lưu ý quan trọng

Hydra và Kratos không tự nối với nhau chỉ bằng Helm values. Cần một Login App/Auth App tự viết để bridge:

- đọc `login_challenge` từ Hydra
- dùng Kratos browser login/session
- gọi Hydra Admin API accept login request
- accept consent hoặc skip consent
- callback đổi code lấy token

## Kiểm tra nhanh

```bash
kubectl get pods -n platform | grep -E 'hydra|kratos'
kubectl get ingress -n platform
```

Kiểm tra OAuth client:

```bash
kubectl exec -n platform deploy/hydra -- \
  hydra get oauth2-client arda-shell \
  --endpoint http://127.0.0.1:4445
```


kubectl exec -it -n platform deploy/hydra -- \
  hydra migrate sql up -e --yes
