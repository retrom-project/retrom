import { cache } from "react";
import { backendJSON } from "@/lib/server-backend";
import type { AuthContext } from "./types";

export const loadAuthContext = cache(() => backendJSON<AuthContext>("/api/v1/auth/context"));
