# GEMINI.md — android/

> Guidelines for AI agents and developers working in this directory. Also consult
> the root **[GEMINI.md](../GEMINI.md)** (specs, core conventions, architecture)
> before editing any code.

## Purpose

Native **Android app (Kotlin + Jetpack Compose + Material 3)** for FocusGuard.
Provides a high-integrity, rootless distraction blocker directly on smartphones:
- **Website blocking**: Local DNS interception via `android.net.VpnService` (local sinkhole at `0.0.0.0`, no external proxy or latency).
- **Application blocking**: Detection of foreground denylisted applications via `android.accessibilityservice.AccessibilityService`.
- **Interceptor overlay**: Focus screen displaying remaining timer, motivational quotes, and Box Breathing guidance (4-4-4-4) rendered via `TYPE_APPLICATION_OVERLAY`.
- **Active session tamper resistance**: While a focus session is active, stopping or pausing the session is prohibited; uninstallation/deactivation is only allowed during idle states.

---

## Directory structure

| Path | Purpose |
|---|---|
| `build.gradle.kts` | Root Gradle configuration (Android Application, Kotlin Android, and Compose plugins) |
| `settings.gradle.kts` | Central repository declarations and `:app` module inclusion |
| `gradle.properties` | Gradle JVM configurations and AndroidX flags |
| `gradle/libs.versions.toml` | **Version Catalog**: Centralized dependency and plugin versions (AGP 8.4.2, Kotlin 2.0.0, Compose BOM) |
| `gradle/wrapper/` | Gradle Wrapper 8.7 binaries and configuration |
| `app/build.gradle.kts` | Application module build spec (`compileSdk 34`, `minSdk 26`, Compose and Material 3 dependencies) |
| `app/proguard-rules.pro` | R8 / ProGuard obfuscation and optimization rules |
| `app/src/main/AndroidManifest.xml` | Android manifest (`VpnService`, `AccessibilityService`, `Overlay`, `ForegroundService`) |
| `app/src/main/java/com/focusguard/app/MainActivity.kt` | Main Compose dashboard entry point |
| `app/src/main/java/com/focusguard/app/ui/theme/` | FocusGuard design system tokens (`Color.kt`, `Theme.kt`, `Type.kt`) |
| `app/src/main/res/` | Android XML resources, adaptive icons, and strings |

---

## Specific conventions

1. **Strict TDD (Test-Driven Development) — MANDATORY**:
   All features (blocking rules, schedulers, DNS parsers, timers, or bug fixes) MUST strictly adhere to the Red → Green → Refactor workflow. Write unit/instrumented tests before writing production code.
2. **Zero Root**:
   Operate strictly within standard Android user-space. Never attempt to read `/etc/hosts` or invoke `iptables` via shell commands (blocked by SELinux without root).
3. **Local network blocking via `VpnService`**:
   - Establish a local virtual interface (`10.0.0.1`).
   - Route DNS packets (port 53 UDP/TCP) locally without forwarding traffic to third-party VPN servers.
   - Sinkhole blocked domains with `0.0.0.0` responses; resolve allowed domains through the system upstream resolver (`1.1.1.1` or carrier/Wi-Fi DNS).
4. **App blocking via `AccessibilityService`**:
   - Monitor `TYPE_WINDOW_STATE_CHANGED` window events.
   - Upon detecting a denylisted package name, immediately launch the interceptor overlay or redirect the user to the home launcher.
5. **Interceptor Overlay**:
   - Render using `WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY` to physically block interaction with restricted apps.
   - Maintain visual parity with the desktop FocusGuard design language (Dark Slate `#090A0F`, Emerald `#10B981`, Alert `#EF4444`).
6. **Active session integrity**:
   - Do NOT provide manual pause, skip, or cancel buttons during an active block session.
   - If the user navigates to the app details screen in system Settings during an active session, accessibility should intercept and redirect to Home.
7. **Monotonic hardware clock**:
   - Never use `System.currentTimeMillis()` for session expiration calculations (user can manipulate system clock).
   - Always rely on `SystemClock.elapsedRealtime()` (hardware uptime ticks since boot).
8. **Foreground service lifecycle**:
   - Active blocks must be bound to a persistent `ForegroundService` with a high-priority notification to protect against system OOM kills.
9. **Session log**:
   - Update `../docs/session-log/YYYY-MM-DD.md` at the end of each session.

---

## Known Android gotchas

- **Android 14 (API 34) Foreground Service Types**:
  Foreground services must declare explicit `android:foregroundServiceType` attributes in `AndroidManifest.xml` (e.g., `specialUse` or `systemExempted`).
- **Doze mode and OEM battery optimization**:
  Aggressive battery optimization (Samsung, Xiaomi, Huawei) may suspend VPN sockets when the screen locks. Prompt for battery optimization exemption (`ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`).
- **Single active VPN restriction**:
  Android permits only one active `VpnService` at a time. If the user activates an external VPN, FocusGuard's VPN is disconnected. The app must observe this state and alert the user immediately.
- **Accessibility service state**:
  The system or OS restart may occasionally disable accessibility services. Check permission status in `onResume()` and notify the user if re-activation is needed.

---

## Validation

- **Build APKs (Debug & Release)**:
  ```bash
  cd android
  ./gradlew assembleDebug assembleRelease
  ```
- **Run unit tests**:
  ```bash
  cd android
  ./gradlew test
  ```
- **Lint verification**:
  ```bash
  cd android
  ./gradlew lint
  ```
