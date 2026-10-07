"use client";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";
import type { ReactNode } from "react";
import { api, configureClient, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
type AuthState = {
  context: Schema<"AuthContext"> | null;
  error: string;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
  accept: (context: Schema<"AuthContext">) => void;
};
const Auth = createContext<AuthState | null>(null);
export function AuthProvider({ children }: { children: ReactNode }) {
  const [context, setContext] = useState<Schema<"AuthContext"> | null>(null);
  const [error, setError] = useState("");
  const accept = useCallback((value: Schema<"AuthContext">) => {
    configureClient(value.csrfToken);
    setContext(value);
    setError("");
  }, []);
  const refresh = useCallback(async () => {
    try {
      accept(result(await api.GET("/api/v1/auth/context")));
    } catch (failure) {
      setError(
        failure instanceof Error ? failure.message : "无法确认账号状态。",
      );
    }
  }, [accept]);
  const logout = useCallback(async () => {
    const response = await api.POST("/api/v1/auth/logout");
    if (response.error) {
      setError(response.error.message);
      return;
    }
    await refresh();
  }, [refresh]);
  useEffect(() => {
    const timer = setTimeout(() => void refresh(), 0);
    return () => clearTimeout(timer);
  }, [refresh]);
  return (
    <Auth.Provider value={{ context, error, refresh, logout, accept }}>
      {children}
    </Auth.Provider>
  );
}
export function useAuth() {
  const value = useContext(Auth);
  if (!value) {
    throw new Error("AuthProvider required");
  }
  return value;
}
