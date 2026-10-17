import { createContext } from "react";
import type { AuthState } from "./types";

export const AuthContext = createContext<AuthState | null>(null);
