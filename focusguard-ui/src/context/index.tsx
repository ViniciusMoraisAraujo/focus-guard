// Barrel do contexto: providers granulares (SRP) + composição AppProvider.
//
// Estrutura:
//   <AuthProvider>  — auth, login, logout, sessão expirada (useAuth)
//     <DataProvider> — daemonUp, status, presets, stats, refresh (useData)
//
// Telas devem usar os hooks granulares (useAuth/useData) para não re-renderizar
// à toa; useApp (compat) combina os dois e fica para consumidores legados.
import type { ReactNode } from "react";
import { AuthProvider, useAuth } from "./auth-context";
import { DataProvider, useData } from "./data-context";
export { AuthProvider, useAuth, type LoginResult } from "./auth-context";
export { DataProvider, useData } from "./data-context";
export { NOT_AUTHENTICATED, type AuthState, type Screen } from "./types";

/**
 * useApp (compat): combina useAuth + useData na mesma superfície do antigo
 * AppProvider. Mantido para testes de caracterização e consumidores legados;
 * código novo deve usar os hooks granulares.
 */
export function useApp() {
  return { ...useAuth(), ...useData() };
}

/** AppProvider = composição dos providers granulares (ordem importa). */
export function AppProvider({ children }: { children: ReactNode }) {
  return (
    <AuthProvider>
      <DataProvider>{children}</DataProvider>
    </AuthProvider>
  );
}
