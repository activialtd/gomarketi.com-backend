// RS256 cross-language interop smoke test. Confirms Go's golang-jwt/jwt/v5
// and Node's jsonwebtoken agree on the same RSA keypair before anything else
// in admin-api depends on that assumption. Run first in Phase 1.
import { execFileSync } from "node:child_process";
import path from "node:path";
import jwt from "jsonwebtoken";
import { config } from "../src/config.js";

const helperDir = path.join(import.meta.dirname, "jwt-helper");

function goRun(args: string[]): string {
  return execFileSync("go", ["run", ".", ...args], { cwd: helperDir, encoding: "utf8" });
}

let failures = 0;

function check(label: string, fn: () => void) {
  try {
    fn();
    console.log(`PASS: ${label}`);
  } catch (err) {
    failures++;
    console.error(`FAIL: ${label} —`, err instanceof Error ? err.message : err);
  }
}

// 1. Go mints, Node verifies — confirms Node can consume tokens the existing
// Go auth service issues.
check("Go → Node", () => {
  const token = goRun(["mint"]).trim();
  const decoded = jwt.verify(token, config.jwt.publicKey, { algorithms: ["RS256"] });
  if (typeof decoded === "string" || decoded.sub !== "smoke-test-user-id") {
    throw new Error("unexpected decoded payload");
  }
});

// 2. Node mints, Node verifies — the actual runtime path admin-api uses.
check("Node → Node", () => {
  const token = jwt.sign({ sub: "node-smoke-test", is_admin: true, admin_role: "super_admin" }, config.jwt.privateKey, {
    algorithm: "RS256",
    expiresIn: 60,
  });
  const decoded = jwt.verify(token, config.jwt.publicKey, { algorithms: ["RS256"] });
  if (typeof decoded === "string" || decoded.sub !== "node-smoke-test") {
    throw new Error("unexpected decoded payload");
  }
});

// 3. Node mints, Go verifies — confirms no PKCS#1/PKCS#8/PKIX encoding
// mismatch the other direction.
check("Node → Go", () => {
  const token = jwt.sign({ sub: "node-to-go-smoke-test" }, config.jwt.privateKey, {
    algorithm: "RS256",
    expiresIn: 60,
  });
  const out = goRun(["verify", token]);
  const parsed = JSON.parse(out);
  if (parsed.sub !== "node-to-go-smoke-test") {
    throw new Error(`unexpected Go-side decode: ${out}`);
  }
});

if (failures > 0) {
  console.error(`\n${failures} check(s) failed`);
  process.exit(1);
}
console.log("\nAll RS256 interop checks passed.");
