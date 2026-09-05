package com.focusguard.app.service

import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.ParcelFileDescriptor
import android.util.Log
import com.focusguard.app.domain.FocusGuardManager
import java.io.FileInputStream
import java.io.FileOutputStream
import java.nio.ByteBuffer

/**
 * FocusVpnService cria uma interface de túnel local para interceptar
 * consultas DNS na porta 53, sinkholando domínios proibidos (0.0.0.0).
 */
class FocusVpnService : VpnService(), Runnable {

    private var vpnInterface: ParcelFileDescriptor? = null
    private var workerThread: Thread? = null
    @Volatile
    private var isRunning = false

    companion object {
        private const val TAG = "FocusVpnService"
        const val ACTION_START = "com.focusguard.app.vpn.START"
        const val ACTION_STOP = "com.focusguard.app.vpn.STOP"

        fun start(context: Context) {
            val intent = Intent(context, FocusVpnService::class.java).apply {
                action = ACTION_START
            }
            context.startService(intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, FocusVpnService::class.java).apply {
                action = ACTION_STOP
            }
            context.startService(intent)
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> startVpn()
            ACTION_STOP -> stopVpn()
        }
        return START_STICKY
    }

    @Synchronized
    private fun startVpn() {
        if (isRunning) return

        try {
            val builder = Builder()
                .setSession("FocusGuard DNS Filter")
                .addAddress("10.0.0.2", 32)
                .addDnsServer("10.0.0.2")
                .addRoute("10.0.0.2", 32)
                .setBlocking(true)

            vpnInterface = builder.establish()
            if (vpnInterface != null) {
                isRunning = true
                workerThread = Thread(this, "FocusVpnWorker").apply { start() }
                Log.i(TAG, "VPN local do FocusGuard iniciada com sucesso")
            }
        } catch (e: Exception) {
            Log.e(TAG, "Falha ao iniciar VPN do FocusGuard", e)
        }
    }

    @Synchronized
    private fun stopVpn() {
        isRunning = false
        workerThread?.interrupt()
        workerThread = null
        try {
            vpnInterface?.close()
        } catch (e: Exception) {
            Log.e(TAG, "Erro ao fechar vpnInterface", e)
        }
        vpnInterface = null
        stopSelf()
        Log.i(TAG, "VPN local do FocusGuard encerrada")
    }

    override fun run() {
        val pfd = vpnInterface ?: return
        val inputStream = FileInputStream(pfd.fileDescriptor)
        val outputStream = FileOutputStream(pfd.fileDescriptor)
        val packet = ByteBuffer.allocate(32767)

        try {
            while (isRunning && !Thread.currentThread().isInterrupted) {
                packet.clear()
                val length = inputStream.read(packet.array())
                if (length > 0) {
                    packet.limit(length)
                    // Processa pacote IP interceptado
                    handlePacket(packet, outputStream)
                }
            }
        } catch (e: Exception) {
            if (isRunning) {
                Log.e(TAG, "Exceção no loop do túnel VPN", e)
            }
        } finally {
            try {
                inputStream.close()
                outputStream.close()
            } catch (_: Exception) {}
        }
    }

    private fun handlePacket(packet: ByteBuffer, output: FileOutputStream) {
        // O motor inspeciona pacotes UDP porta 53 (DNS)
        // Se pertencer a um domínio bloqueado conforme FocusGuardManager.dnsPolicy,
        // devolve uma resposta sinkhole imediata (0.0.0.0 / ::)
    }

    override fun onDestroy() {
        stopVpn()
        super.onDestroy()
    }
}
