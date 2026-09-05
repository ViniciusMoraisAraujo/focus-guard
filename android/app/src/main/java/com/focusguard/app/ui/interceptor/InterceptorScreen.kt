package com.focusguard.app.ui.interceptor

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.HourglassBottom
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.focusguard.app.domain.BreathingCycle
import com.focusguard.app.domain.FocusGuardManager
import com.focusguard.app.ui.theme.BgDark
import com.focusguard.app.ui.theme.BorderDark
import com.focusguard.app.ui.theme.CyanAccent
import com.focusguard.app.ui.theme.DangerRed
import com.focusguard.app.ui.theme.EmeraldPrimary
import com.focusguard.app.ui.theme.SurfaceDark
import com.focusguard.app.ui.theme.SurfaceVariantDark
import com.focusguard.app.ui.theme.TextMuted
import com.focusguard.app.ui.theme.TextPrimary
import com.focusguard.app.ui.theme.TextSecondary
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive

@Composable
fun InterceptorScreen(
    blockedPackage: String,
    onReturnHome: () -> Unit
) {
    val breathingCycle = remember { BreathingCycle(phaseDurationSec = 4) }
    var elapsedMs by remember { mutableLongStateOf(0L) }
    var remainingMs by remember { mutableLongStateOf(FocusGuardManager.session.remainingTimeMs()) }

    // Loop para atualizar respiração (60fps suave) e contador de tempo restante
    LaunchedEffect(Unit) {
        val startTime = System.currentTimeMillis()
        while (isActive) {
            elapsedMs = System.currentTimeMillis() - startTime
            remainingMs = FocusGuardManager.session.remainingTimeMs()
            delay(50L)
        }
    }

    val breathingState = breathingCycle.getPhaseAt(elapsedMs)
    val appDisplayName = extractAppDisplayName(blockedPackage)

    Scaffold(
        containerColor = BgDark
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.SpaceBetween
        ) {
            // 1. Cabeçalho de Alerta
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier.padding(top = 16.dp)
            ) {
                Box(
                    modifier = Modifier
                        .size(64.dp)
                        .clip(CircleShape)
                        .background(DangerRed.copy(alpha = 0.15f)),
                    contentAlignment = Alignment.Center
                ) {
                    Icon(
                        imageVector = Icons.Default.Shield,
                        contentDescription = "Bloqueado",
                        tint = DangerRed,
                        modifier = Modifier.size(36.dp)
                    )
                }

                Spacer(modifier = Modifier.height(16.dp))

                Text(
                    text = "$appDisplayName Bloqueado",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                    color = TextPrimary
                )

                Spacer(modifier = Modifier.height(6.dp))

                Text(
                    text = "Este aplicativo está restrito durante a sua sessão de foco.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextSecondary
                )

                Spacer(modifier = Modifier.height(14.dp))

                // Card com Tempo Restante
                Card(
                    colors = CardDefaults.cardColors(containerColor = SurfaceDark),
                    shape = RoundedCornerShape(12.dp),
                    border = BorderStroke(1.dp, BorderDark)
                ) {
                    Text(
                        text = "Tempo restante: ${formatRemaining(remainingMs)}",
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = CyanAccent,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)
                    )
                }
            }

            // 2. Exercício de Respiração Quadrada (Box Breathing 4-4-4-4)
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier.padding(vertical = 24.dp)
            ) {
                Text(
                    text = "Exercício de Respiração (4-4-4-4)",
                    style = MaterialTheme.typography.labelSmall,
                    color = TextMuted,
                    letterSpacing = 1.sp
                )

                Spacer(modifier = Modifier.height(20.dp))

                // Caixa de Respiração Pulsante
                Box(
                    modifier = Modifier
                        .size(150.dp)
                        .scale(breathingState.scale)
                        .clip(RoundedCornerShape(28.dp))
                        .background(SurfaceDark)
                        .padding(2.dp),
                    contentAlignment = Alignment.Center
                ) {
                    Box(
                        modifier = Modifier
                            .fillMaxSize()
                            .clip(RoundedCornerShape(26.dp))
                            .background(EmeraldPrimary.copy(alpha = 0.15f)),
                        contentAlignment = Alignment.Center
                    ) {
                        Text(
                            text = breathingState.instruction,
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold,
                            color = EmeraldPrimary,
                            fontSize = 15.sp
                        )
                    }
                }

                Spacer(modifier = Modifier.height(24.dp))

                Text(
                    text = "Respire com calma antes de decidir continuar.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextSecondary
                )
            }

            // 3. Botão de Retorno à Home
            Button(
                onClick = onReturnHome,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(54.dp),
                shape = RoundedCornerShape(14.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = EmeraldPrimary,
                    contentColor = BgDark
                )
            ) {
                Text(
                    text = "Voltar ao Foco",
                    fontSize = 16.sp,
                    fontWeight = FontWeight.Bold
                )
            }
        }
    }
}

private fun extractAppDisplayName(pkg: String): String {
    return when {
        pkg.contains("instagram") -> "Instagram"
        pkg.contains("musically") || pkg.contains("tiktok") -> "TikTok"
        pkg.contains("youtube") -> "YouTube"
        pkg.contains("twitter") || pkg.contains("x.android") -> "X (Twitter)"
        pkg.contains("reddit") -> "Reddit"
        pkg.contains("facebook") -> "Facebook"
        else -> "Aplicativo"
    }
}

private fun formatRemaining(ms: Long): String {
    val totalSec = ms / 1000L
    val min = totalSec / 60L
    val sec = totalSec % 60L
    return String.format("%02d:%02d", min, sec)
}
