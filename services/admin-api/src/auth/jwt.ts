import jwt from "jsonwebtoken";
import { randomUUID } from "node:crypto";
import { config } from "../config.js";

export type AdminRole = "agent" | "supervisor" | "super_admin";

// Deliberately new claim names (is_admin/admin_role) rather than overloading
// the Go auth service's shared/pkg/jwt.Claims (IsBuyer/IsVendor/StaffRole) —
// admin tokens are never parsed by that struct, so it needs zero changes.
export interface AdminClaims {
  sub: string;
  email: string;
  full_name: string;
  is_admin: true;
  admin_role: AdminRole;
  jti: string;
  iat: number;
  exp: number;
}

export function issueAdminToken(input: {
  id: string;
  email: string;
  fullName: string;
  role: AdminRole;
}): string {
  const payload: Omit<AdminClaims, "iat" | "exp"> = {
    sub: input.id,
    email: input.email,
    full_name: input.fullName,
    is_admin: true,
    admin_role: input.role,
    jti: randomUUID(),
  };
  return jwt.sign(payload, config.jwt.privateKey, {
    algorithm: "RS256",
    expiresIn: config.jwt.accessTokenTTLSeconds,
  });
}

export function verifyAdminToken(token: string): AdminClaims {
  const decoded = jwt.verify(token, config.jwt.publicKey, { algorithms: ["RS256"] });
  if (typeof decoded === "string" || !decoded.is_admin) {
    throw new Error("not an admin token");
  }
  return decoded as AdminClaims;
}
