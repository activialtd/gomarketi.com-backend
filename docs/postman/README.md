# GoMarketi API — Postman collection

For the frontend team, who don't have access to this repo's code.

## Import

1. Postman → Import → drop in `GoMarketi-API.postman_collection.json`.
2. Import both environment files too (`GoMarketi-Staging.postman_environment.json`,
   `GoMarketi-Production.postman_environment.json`), then pick one from the
   environment dropdown (top-right).
3. In Postman Settings → General, turn on **"Automatically follow redirects"**
   and make sure cookies are enabled for the domain — login/OTP verify/OAuth
   endpoints set an HttpOnly `refresh_token` cookie that Postman's cookie jar
   handles for you automatically on `/v1/auth/token/refresh` and
   `/v1/auth/logout`.

## Auth flow

1. Run **Auth → Login** (or Register / Verify OTP / Google / Apple).
2. Copy `access_token` from the response into the environment's
   `access_token` variable (top-right eye icon → edit).
3. Every other request already sends `Authorization: Bearer {{access_token}}`
   automatically (set at the collection level).
4. Access tokens expire in 15 minutes — when requests start 401ing, run
   **Auth → Refresh Token** (no body needed, uses the cookie) and update
   `access_token` again.

## Things to know

- **Public vs authenticated:** requests under each service's `public/*` path
  (and a few others, marked "no auth" in Postman) don't need a token.
  Everything else does.
- **Money fields** (`price_kobo`, `total_kobo`, `amount_kobo`, etc.) are
  always **integers in kobo** (1 Naira = 100 kobo) — never floats, never
  Naira directly. Format for display client-side.
- **Errors** are always `{ "error": "..." }`, or for validation failures,
  `{ "error": "...", "fields": [{ "field": "...", "message": "..." }] }`.
- **File uploads** are `multipart/form-data`, not JSON — see
  "Storefront → Upload Store Asset" for the pattern (presigned direct-to-R2
  upload is also available via "Storefront → Presign Upload" for product
  images etc.).
- **Real-time order/wallet updates**: `GET /v1/orders/ws` is a WebSocket,
  not a REST call — Postman can open it under a WebSocket request, but pass
  the token as `?token=<access_token>` in the URL since browsers/WS clients
  can't set custom headers on the handshake.
