package com.focusguard.app.ui.onboarding

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.BorderStroke
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
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Layers
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VpnKey
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import com.focusguard.app.domain.PermissionManager
import com.focusguard.app.domain.PermissionStatus
import com.focusguard.app.ui.theme.BgDark
import com.focusguard.app.ui.theme.BorderDark
import com.focusguard.app.ui.theme.CyanAccent
import com.focusguard.app.ui.theme.EmeraldDark
import com.focusguard.app.ui.theme.EmeraldPrimary
import com.focusguard.app.ui.theme.SurfaceDark
import com.focusguard.app.ui.theme.SurfaceVariantDark
import com.focusguard.app.ui.theme.TextMuted
import com.focusguard.app.ui.theme.TextPrimary
import com.focusguard.app.ui.theme.TextSecondary

@Composable
fun OnboardingScreen(
    onFinished: () -> Unit
) {
    val context = LocalContext.current
    var permissionStatus by remember { mutableStateOf(PermissionManager.checkPermissions(context)) }

    // Atualiza automaticamente quando o usuário volta das telas de Configurações do Android
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == LifecycleEvent.ON_RESUME) {
                permissionStatus = PermissionManager.checkPermissions(context)
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose {
            lifecycleOwner.lifecycle.removeObserver(observer)
        }
    }

    // Launcher para a caixa de diálogo nativa de VPN do Android
    val vpnLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.StartActivityForResult()
    ) {
        permissionStatus = PermissionManager.checkPermissions(context)
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
            // Header
            Spacer(modifier = Modifier.height(16.dp))
            Box(
                modifier = Modifier
                    .size(56.dp)
                    .clip(RoundedCornerShape(16.dp))
                    .background(EmeraldPrimary.copy(alpha = 0.15f)),
                contentAlignment = Alignment.Center
            ) {
                Icon(
                    imageVector = Icons.Default.Security,
                    contentDescription = null,
                    tint = EmeraldPrimary,
                    modifier = Modifier.size(32.dp)
                )
            }

            Spacer(modifier = Modifier.height(16.dp))
            Text(
                text = "Configuração Inicial",
                style = MaterialTheme.typography.headlineMedium,
                fontWeight = FontWeight.Bold,
                color = TextPrimary
            )
            Spacer(modifier = Modifier.height(6.dp))
            Text(
                text = "Para bloquear distrações sem exigir root, o FocusGuard precisa de 3 permissões do sistema.",
                style = MaterialTheme.typography.bodyMedium,
                color = TextSecondary,
                modifier = Modifier.padding(horizontal = 8.dp)
            )

            Spacer(modifier = Modifier.height(20.dp))

            // Barra de Progresso
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween
            ) {
                Text(
                    text = "Progresso da Configuração",
                    style = MaterialTheme.typography.labelSmall,
                    color = TextMuted
                )
                Text(
                    text = "${permissionStatus.grantedCount()} de 3 concluídos",
                    style = MaterialTheme.typography.labelSmall,
                    color = if (permissionStatus.isAllGranted()) EmeraldPrimary else TextSecondary,
                    fontWeight = FontWeight.SemiBold
                )
            }
            Spacer(modifier = Modifier.height(8.dp))
            LinearProgressIndicator(
                progress = { permissionStatus.progress() },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(8.dp)
                    .clip(CircleShape),
                color = EmeraldPrimary,
                trackColor = SurfaceVariantDark
            )

            Spacer(modifier = Modifier.height(24.dp))

            // 1. Permissão de Acessibilidade
            PermissionCard(
                title = "1. Monitor de Aplicativos",
                description = "Detecta instantaneamente quando um app bloqueado é aberto.",
                icon = Icons.Default.Visibility,
                isGranted = permissionStatus.accessibilityGranted,
                onClick = {
                    val intent = Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)
                    context.startActivity(intent)
                }
            )

            Spacer(modifier = Modifier.height(14.dp))

            // 2. Permissão de Overlay (Sobreposição de Tela)
            PermissionCard(
                title = "2. Tela de Bloqueio (Overlay)",
                description = "Permite exibir a tela de respiração sobre os apps proibidos.",
                icon = Icons.Default.Layers,
                isGranted = permissionStatus.overlayGranted,
                onClick = {
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                        val intent = Intent(
                            Settings.ACTION_MANAGE_OVERLAY_PERMISSION,
                            Uri.parse("package:${context.packageName}")
                        )
                        context.startActivity(intent)
                    }
                }
            )

            Spacer(modifier = Modifier.height(14.dp))

            // 3. Permissão de VPN Local
            PermissionCard(
                title = "3. Filtro de Navegação (VPN)",
                description = "Bloqueia sites nocivos localmente na porta DNS sem proxy externo.",
                icon = Icons.Default.VpnKey,
                isGranted = permissionStatus.vpnGranted,
                onClick = {
                    val prepareIntent = VpnService.prepare(context)
                    if (prepareIntent != null) {
                        vpnLauncher.launch(prepareIntent)
                    } else {
                        permissionStatus = PermissionManager.checkPermissions(context)
                    }
                }
            )

            Spacer(modifier = Modifier.weight(1f))

            // Botão Continuar
            Button(
                onClick = onFinished,
                enabled = permissionStatus.isAllGranted(),
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
                    text = if (permissionStatus.isAllGranted()) "Concluir e Começar" else "Conceda as Permissões Acima",
                    fontSize = 16.sp,
                    fontWeight = FontWeight.Bold
                )
            }
        }
    }
}

@Composable
fun PermissionCard(
    title: String,
    description: String,
    icon: ImageVector,
    isGranted: Boolean,
    onClick: () -> Unit
) {
    Card(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = SurfaceDark),
        shape = RoundedCornerShape(16.dp),
        border = BorderStroke(1.dp, if (isGranted) EmeraldDark else BorderDark)
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Box(
                modifier = Modifier
                    .size(44.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(
                        if (isGranted) EmeraldPrimary.copy(alpha = 0.15f)
                        else SurfaceVariantDark
                    ),
                contentAlignment = Alignment.Center
            ) {
                Icon(
                    imageVector = icon,
                    contentDescription = null,
                    tint = if (isGranted) EmeraldPrimary else TextSecondary,
                    modifier = Modifier.size(24.dp)
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

            Spacer(modifier = Modifier.width(8.dp))

            if (isGranted) {
                Icon(
                    imageVector = Icons.Default.CheckCircle,
                    contentDescription = "Concedido",
                    tint = EmeraldPrimary,
                    modifier = Modifier.size(26.dp)
                )
            } else {
                Box(
                    modifier = Modifier
                        .size(32.dp)
                        .clip(CircleShape)
                        .background(SurfaceVariantDark),
                    contentAlignment = Alignment.Center
                ) {
                    Icon(
                        imageVector = Icons.Default.ChevronRight,
                        contentDescription = "Configurar",
                        tint = TextSecondary,
                        modifier = Modifier.size(20.dp)
                    )
                }
            }
        }
    }
}
