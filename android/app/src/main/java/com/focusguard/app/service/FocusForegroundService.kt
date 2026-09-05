package com.focusguard.app.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import com.focusguard.app.MainActivity
import com.focusguard.app.R
import com.focusguard.app.domain.FocusGuardManager
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * FocusForegroundService mantém o FocusGuard em primeiro plano com notificação
 * persistente para garantir imunidade contra encerramento pelo sistema operacional.
 */
class FocusForegroundService : Service() {

    private val serviceScope = CoroutineScope(Dispatchers.Default + Job())
    private var updateJob: Job? = null

    companion object {
        const val CHANNEL_ID = "focusguard_active_session"
        const val NOTIFICATION_ID = 48901

        const val ACTION_START = "com.focusguard.app.action.START_FOCUS"
        const val ACTION_STOP = "com.focusguard.app.action.STOP_FOCUS"
        const val EXTRA_DURATION_MS = "extra_duration_ms"

        fun start(context: Context, durationMs: Long) {
            val intent = Intent(context, FocusForegroundService::class.java).apply {
                action = ACTION_START
                putExtra(EXTRA_DURATION_MS, durationMs)
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stop(context: Context) {
            val intent = Intent(context, FocusForegroundService::class.java).apply {
                action = ACTION_STOP
            }
            context.startService(intent)
        }
    }

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> {
                val durationMs = intent.getLongExtra(EXTRA_DURATION_MS, 25 * 60 * 1000L)
                FocusGuardManager.startFocus(durationMs)
                startForeground(NOTIFICATION_ID, buildNotification("Sessão iniciada"))
                startPeriodicUpdates()
            }
            ACTION_STOP -> {
                if (FocusGuardManager.session.canCancel()) {
                    FocusGuardManager.session.stop()
                    stopPeriodicUpdates()
                    stopForeground(STOP_FOREGROUND_REMOVE)
                    stopSelf()
                }
            }
        }
        return START_STICKY
    }

    private fun startPeriodicUpdates() {
        updateJob?.cancel()
        updateJob = serviceScope.launch {
            while (isActive) {
                val remainingMs = FocusGuardManager.session.remainingTimeMs()
                if (remainingMs <= 0L) {
                    FocusGuardManager.checkStatus()
                    stopForeground(STOP_FOREGROUND_REMOVE)
                    stopSelf()
                    break
                }
                val notificationManager = getSystemService(NotificationManager::class.java)
                notificationManager?.notify(NOTIFICATION_ID, buildNotification(formatRemaining(remainingMs)))
                delay(1000L)
            }
        }
    }

    private fun stopPeriodicUpdates() {
        updateJob?.cancel()
        updateJob = null
    }

    private fun buildNotification(statusText: String): Notification {
        val launchIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        val pendingIntent = PendingIntent.getActivity(
            this,
            0,
            launchIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("FocusGuard — Modo de Foco")
            .setContentText("Tempo restante: $statusText")
            .setSmallIcon(R.drawable.ic_launcher_foreground)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .build()
    }

    private fun formatRemaining(ms: Long): String {
        val totalSec = ms / 1000L
        val min = totalSec / 60L
        val sec = totalSec % 60L
        return String.format("%02d:%02d", min, sec)
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = getString(R.string.notification_channel_desc)
                setShowBadge(false)
            }
            val notificationManager = getSystemService(NotificationManager::class.java)
            notificationManager?.createNotificationChannel(channel)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        stopPeriodicUpdates()
        super.onDestroy()
    }
}
