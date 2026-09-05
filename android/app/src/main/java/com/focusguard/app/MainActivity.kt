package com.focusguard.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Block
import androidx.compose.material.icons.filled.HourglassEmpty
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.Timer
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.focusguard.app.ui.theme.BgDark
import com.focusguard.app.ui.theme.BorderDark
import com.focusguard.app.ui.theme.CyanAccent
import com.focusguard.app.ui.theme.EmeraldPrimary
import com.focusguard.app.ui.theme.FocusGuardTheme
import com.focusguard.app.ui.theme.SurfaceDark
import com.focusguard.app.ui.theme.SurfaceVariantDark
import com.focusguard.app.ui.theme.TextMuted
import com.focusguard.app.ui.theme.TextPrimary
import com.focusguard.app.ui.theme.TextSecondary

import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.platform.LocalContext
import com.focusguard.app.domain.FocusGuardManager
import com.focusguard.app.service.FocusForegroundService

import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import com.focusguard.app.domain.PermissionManager
import com.focusguard.app.ui.onboarding.OnboardingScreen

import com.focusguard.app.ui.apps.AppsScreen
import com.focusguard.app.ui.components.QuickTimerDialog
import com.focusguard.app.ui.pomodoro.PomodoroScreen

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            FocusGuardTheme {
                val context = LocalContext.current
                var showOnboarding by remember {
                    mutableStateOf(!PermissionManager.checkPermissions(context).isAllGranted())
                }
                var currentScreen by remember { mutableStateOf("main") }

                if (showOnboarding) {
                    OnboardingScreen(onFinished = { showOnboarding = false })
                } else {
                    when (currentScreen) {
                        "apps" -> AppsScreen(onBack = { currentScreen = "main" })
                        "pomodoro" -> PomodoroScreen(onBack = { currentScreen = "main" })
                        else -> FocusGuardMainScreen(
                            onOpenApps = { currentScreen = "apps" },
                            onOpenPomodoro = { currentScreen = "pomodoro" }
                        )
                    }
                }
            }
        }
    }
}

@Composable
fun FocusGuardMainScreen(
    onOpenApps: () -> Unit = {},
    onOpenPomodoro: () -> Unit = {}
) {
    val isFocusActive by FocusGuardManager.isFocusActive.collectAsState()
    val context = LocalContext.current
    var showQuickTimerDialog by remember { mutableStateOf(false) }

    if (showQuickTimerDialog) {
        QuickTimerDialog(
            onDismiss = { showQuickTimerDialog = false },
            onStartFocus = { durationMs ->
                FocusForegroundService.start(context, durationMs)
            }
        )
    }

    Scaffold(
        containerColor = BgDark
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .padding(20.dp),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            // Header: FocusGuard Logo & Title
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 12.dp, bottom = 24.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                Box(
                    modifier = Modifier
                        .size(44.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .background(EmeraldPrimary.copy(alpha = 0.15f)),
                    contentAlignment = Alignment.Center
                ) {
                    Icon(
                        imageVector = Icons.Default.Security,
                        contentDescription = "FocusGuard",
                        tint = EmeraldPrimary,
                        modifier = Modifier.size(26.dp)
                    )
                }
                Spacer(modifier = Modifier.width(12.dp))
                Column {
                    Text(
                        text = "FocusGuard",
                        style = MaterialTheme.typography.headlineMedium,
                        fontWeight = FontWeight.Bold,
                        color = TextPrimary
                    )
                    Text(
                        text = "Bloqueador de Alta Integridade",
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextSecondary
                    )
                }
            }

            // Status Card
            Card(
                modifier = Modifier.fillMaxWidth(),
                colors = CardDefaults.cardColors(containerColor = SurfaceDark),
                shape = RoundedCornerShape(16.dp),
                border = androidx.compose.foundation.BorderStroke(1.dp, BorderDark)
            ) {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(20.dp),
                    horizontalAlignment = Alignment.CenterHorizontally
                ) {
                    Box(
                        modifier = Modifier
                            .size(72.dp)
                            .clip(CircleShape)
                            .background(SurfaceVariantDark),
                        contentAlignment = Alignment.Center
                    ) {
                        Icon(
                            imageVector = Icons.Default.HourglassEmpty,
                            contentDescription = "Status",
                            tint = EmeraldPrimary,
                            modifier = Modifier.size(36.dp)
                        )
                    }
                    Spacer(modifier = Modifier.height(16.dp))
                    Text(
                        text = if (isFocusActive) "Sessão de Foco Ativa" else "Pronto para Focar",
                        style = MaterialTheme.typography.headlineMedium,
                        fontSize = 20.sp,
                        fontWeight = FontWeight.SemiBold,
                        color = if (isFocusActive) EmeraldPrimary else TextPrimary
                    )
                    Spacer(modifier = Modifier.height(6.dp))
                    Text(
                        text = if (isFocusActive) "Distrações e aplicativos proibidos estão bloqueados" else "Nenhuma sessão ativa no momento",
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextMuted
                    )
                }
            }

            Spacer(modifier = Modifier.height(24.dp))

            // Action Section Title
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.Start
            ) {
                Text(
                    text = "Modos de Foco",
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = TextPrimary
                )
            }

            Spacer(modifier = Modifier.height(12.dp))

            // Mode Cards
            ModeCard(
                title = "Pomodoro",
                description = "25m de foco ininterrupto + 5m de descanso",
                icon = Icons.Default.Timer,
                accentColor = EmeraldPrimary,
                onClick = onOpenPomodoro
            )

            Spacer(modifier = Modifier.height(12.dp))

            ModeCard(
                title = "Timer Rápido",
                description = "Defina uma duração personalizada para o bloqueio",
                icon = Icons.Default.HourglassEmpty,
                accentColor = CyanAccent,
                onClick = { showQuickTimerDialog = true }
            )

            Spacer(modifier = Modifier.height(12.dp))

            ModeCard(
                title = "Apps & Sites Bloqueados",
                description = "Gerenciar lista de redes sociais e distrações",
                icon = Icons.Default.Block,
                accentColor = TextSecondary,
                onClick = onOpenApps
            )

            Spacer(modifier = Modifier.weight(1f))

            // Quick Start Button (anti-burla: desabilitado durante sessão ativa)
            Button(
                onClick = {
                    if (!isFocusActive) {
                        FocusForegroundService.start(context, 25 * 60 * 1000L)
                    }
                },
                enabled = !isFocusActive,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(54.dp),
                shape = RoundedCornerShape(14.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = EmeraldPrimary,
                    contentColor = BgDark,
                    disabledContainerColor = SurfaceVariantDark,
                    disabledContentColor = TextMuted
                )
            ) {
                Text(
                    text = if (isFocusActive) "Sessão Ativa (Bloqueio em Andamento)" else "Iniciar Sessão de Foco (25 min)",
                    fontSize = 16.sp,
                    fontWeight = FontWeight.Bold
                )
            }
        }
    }
}

@Composable
fun ModeCard(
    title: String,
    description: String,
    icon: ImageVector,
    accentColor: androidx.compose.ui.graphics.Color,
    onClick: () -> Unit
) {
    Card(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = SurfaceDark),
        shape = RoundedCornerShape(14.dp),
        border = androidx.compose.foundation.BorderStroke(1.dp, BorderDark)
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Box(
                modifier = Modifier
                    .size(40.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(accentColor.copy(alpha = 0.15f)),
                contentAlignment = Alignment.Center
            ) {
                Icon(
                    imageVector = icon,
                    contentDescription = title,
                    tint = accentColor,
                    modifier = Modifier.size(22.dp)
                )
            }
            Spacer(modifier = Modifier.width(14.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = title,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = TextPrimary
                )
                Text(
                    text = description,
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextSecondary
                )
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
fun FocusGuardMainScreenPreview() {
    FocusGuardTheme {
        FocusGuardMainScreen()
    }
}
