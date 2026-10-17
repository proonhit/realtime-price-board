import { useContext } from "react";
import { AuthContext } from "../context";
import type { AuthState } from "../types";

export function useAuth(): AuthState {
  const auth = useContext(AuthContext);
  if (!auth) throw new Error("useAuth must be used inside <AuthProvider>");
  return auth;
}
