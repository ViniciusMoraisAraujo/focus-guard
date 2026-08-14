import { useState, type ReactNode } from "react";
import {
  BookOpen,
  Copy,
  ExternalLink,
  Laptop,
  MonitorCog,
  Network,
  Router,
  SearchCheck,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Screen, ScreenHeader } from "@/components/screen";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { useData } from "@/context";
import { toast } from "@/lib/toast";

// Guia.tsx — manual de configuração do DNS sinkhole em espaço dedicado:
// passos no sistema (Windows), passos genéricos de roteador, guias passo a
// passo por fabricante (ZTE, TP-Link, Huawei, …) e diagnóstico. A tela Rede
// aponta para cá ("Abrir guia completo").
export function Guia() {
  const [vendor, setVendor] = useState("zte");

  return (
    <Screen>
      <ScreenHeader
        title="Guia"
        subtitle="Manual de configuração do DNS sinkhole — sistema, roteador (com guias por fabricante) e diagnóstico."
      />

      {/* IP/MAC desta máquina: os valores da reserva DHCP no roteador */}
      <MachineInfoCard />

      {/* No sistema (Windows) */}
      <Card>
        <CardContent className="flex flex-col gap-3 px-5 py-4">
          <div className="flex items-center gap-2">
            <MonitorCog className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">
              No sistema (Windows)
            </h3>
          </div>
          <ol className="flex list-decimal flex-col gap-2.5 pl-5 text-sm text-muted-foreground">
            <li>
              <strong className="text-foreground">Ligue o sinkhole</strong> — na
              tela <em>Rede</em>, no botão{" "}
              <strong className="text-foreground">Ligar</strong>. O daemon passa
              a escutar na porta 53 em IPv4 e IPv6 ({" "}
              <code className="rounded bg-muted px-1">0.0.0.0:53</code> +{" "}
              <code className="rounded bg-muted px-1">[::]:53</code>) e{" "}
              <strong className="text-foreground">
                abre a porta 53 no firewall automaticamente
              </strong>{" "}
              (regras{" "}
              <code className="rounded bg-muted px-1">
                FocusGuard_DNS_Inbound_UDP/TCP
              </code>
              ).
            </li>
            <li>
              <strong className="text-foreground">Perfil de rede Privada</strong>{" "}
              —{" "}
              <code className="rounded bg-muted px-1">ncpa.cpl</code> → botão
              direito na rede → Propriedades → perfil{" "}
              <strong className="text-foreground">Rede privada</strong>. Em rede{" "}
              <em>Pública</em> o Windows trata a conexão como não confiável e o
              tráfego de entrada pode ficar bloqueado mesmo com a regra criada.
            </li>
            <li>
              <strong className="text-foreground">Porta 53 livre</strong> — se o
              daemon não subir (aviso "Porta 53 em uso"), a causa mais comum no
              Windows é o ICS:{" "}
              <code className="rounded bg-muted px-1">
                sc config SharedAccess start= disabled
              </code>{" "}
              e{" "}
              <code className="rounded bg-muted px-1">
                net stop SharedAccess
              </code>{" "}
              (como Administrador). Confira com{" "}
              <code className="rounded bg-muted px-1">
                netstat -ano | findstr :53
              </code>
              .
            </li>
            <li>
              <strong className="text-foreground">
                (Opcional) Políticas por dispositivo
              </strong>{" "}
              — na seção <em>Dispositivos</em> da tela Rede, defina regras por IP
              (bloquear tudo ou allowlist). Sem regra, o dispositivo segue a
              política global.
            </li>
          </ol>
        </CardContent>
      </Card>

      {/* No roteador (modem) — passos genéricos */}
      <Card>
        <CardContent className="flex flex-col gap-3 px-5 py-4">
          <div className="flex items-center gap-2">
            <Router className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">
              No roteador (modem)
            </h3>
            <Badge variant="secondary" className="ml-auto">
              vale para qualquer fabricante
            </Badge>
          </div>
          <ol className="flex list-decimal flex-col gap-2.5 pl-5 text-sm text-muted-foreground">
            <li>
              <strong className="text-foreground">Fixe o IP do PC no DHCP</strong>{" "}
              — reserva de endereço: MAC da máquina → IP fixo (ex.:{" "}
              <code className="rounded bg-muted px-1">192.168.1.100</code>). Sem
              reserva, o DHCP pode trocar o IP e o sinkhole some da rede.
            </li>
            <li>
              <strong className="text-foreground">DNS primário do DHCP</strong>{" "}
              → o IP fixo do PC (o FocusGuard).
            </li>
            <li>
              <strong className="text-foreground">DNS secundário</strong> → um
              resolver público de confiança (ex.:{" "}
              <code className="rounded bg-muted px-1">1.1.1.1</code>) — se o PC
              cair, a rede continua navegando.
            </li>
            <li>
              <strong className="text-foreground">
                IPv6: desligue o anúncio de DNS do roteador
              </strong>{" "}
              (RDNSS/DHCPv6) ou aponte-o para a máquina. Se o roteador se
              anunciar como DNS via IPv6 (
              <code className="rounded bg-muted px-1">fe80::1</code>), celulares
              e TVs preferem ele e{" "}
              <strong className="text-foreground">burlam o sinkhole</strong>.
            </li>
            <li>
              <strong className="text-foreground">
                Reconecte os dispositivos
              </strong>{" "}
              — desconecte e reconecte o Wi-Fi (ou{" "}
              <code className="rounded bg-muted px-1">ipconfig /renew</code>)
              para pegarem o novo DNS.
            </li>
          </ol>
        </CardContent>
      </Card>

      {/* Guias por fabricante */}
      <Card>
        <CardContent className="flex flex-col gap-4 px-5 py-4">
          <div className="flex items-center gap-2">
            <BookOpen className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">
              Guias por fabricante
            </h3>
            <Badge variant="outline" className="ml-auto font-mono">
              {VENDORS.length} roteadores
            </Badge>
          </div>
          <p className="-mt-3 text-sm text-muted-foreground">
            Onde ficam os menus de DHCP, reserva e DNS em cada painel. Os
            passos genéricos acima valem sempre; estes só mostram onde clicar —
            e cada guia linka para o manual/emulador oficial do fabricante.
          </p>

          <Tabs value={vendor} onValueChange={setVendor}>
            <TabsList className="flex w-full flex-wrap">
              {VENDORS.map((v) => (
                <TabsTrigger key={v.id} value={v.id} className="flex-1">
                  {v.label}
                </TabsTrigger>
              ))}
            </TabsList>

            {VENDORS.map((v) => (
              <TabsContent key={v.id} value={v.id}>
                <div className="flex flex-col gap-3">
                  <p className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Laptop className="size-4 shrink-0" />
                    <span>
                      Acesso: <code className="rounded bg-muted px-1">{v.access}</code>
                      {" — "}login no adesivo do aparelho.
                    </span>
                  </p>
                  {v.manual && (
                    <p className="flex items-center gap-2 text-sm">
                      <ExternalLink className="size-4 shrink-0 text-muted-foreground" />
                      <a
                        href={v.manual.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="font-medium text-primary underline-offset-4 hover:underline"
                      >
                        {v.manual.label}
                      </a>
                    </p>
                  )}
                  <ol className="flex list-decimal flex-col gap-2.5 pl-5 text-sm text-muted-foreground">
                    {v.steps.map((s, i) => (
                      <li key={i}>{s}</li>
                    ))}
                  </ol>
                  {v.note && (
                    <p className="flex items-start gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
                      <Network className="mt-0.5 size-3.5 shrink-0" />
                      {v.note}
                    </p>
                  )}
                  <VendorScreenshot id={v.id} label={v.label} />
                </div>
              </TabsContent>
            ))}
          </Tabs>
        </CardContent>
      </Card>

      {/* Testar e diagnosticar */}
      <Card>
        <CardContent className="flex flex-col gap-3 px-5 py-4">
          <div className="flex items-center gap-2">
            <SearchCheck className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">
              Testar e diagnosticar
            </h3>
          </div>
          <ul className="flex list-disc flex-col gap-2.5 pl-5 text-sm text-muted-foreground">
            <li>
              Na máquina:{" "}
              <code className="rounded bg-muted px-1">
                nslookup google.com 127.0.0.1
              </code>{" "}
              deve responder com IPs reais.
            </li>
            <li>
              De um celular na rede:{" "}
              <code className="rounded bg-muted px-1">
                nslookup google.com &lt;IP-do-PC&gt;
              </code>{" "}
              → mesma resposta (sinkhole resolvendo a rede).
            </li>
            <li>
              Domínio bloqueado responde{" "}
              <code className="rounded bg-muted px-1">0.0.0.0</code> (nunca
              erro) — confirme com um site da sua lista de bloqueio.
            </li>
            <li>
              Celulares sem internet: perfil de rede Público, regra inbound
              ausente ou roteador sem o DNS apontado (seções acima).
            </li>
            <li>
              Máquina sem IPv6: o sinkhole sobe só em IPv4 (normal) — o status
              mostra apenas o endereço v4.
            </li>
            <li>
              Quem está usando o sinkhole: os contadores e a seção{" "}
              <em>Atividade bloqueada</em> da tela Rede mostram o tráfego ao
              vivo.
            </li>
          </ul>
        </CardContent>
      </Card>
    </Screen>
  );
}

// MachineInfoCard — IP e MAC desta máquina na LAN (status do daemon): os
// valores que entram na reserva DHCP do roteador (IP fixo + DNS primário).
// Best-effort: sem daemon/rota, mostra um aviso no lugar dos valores.
function MachineInfoCard() {
  const { status } = useData();
  const lanIP = status?.lan_ip ?? "";
  const lanMAC = status?.lan_mac ?? "";

  const copy = async (text: string, label: string) => {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      toast(`${label} copiado.`, "ok");
    } catch {
      toast("Não foi possível copiar.", "err");
    }
  };

  return (
    <Card>
      <CardContent className="flex flex-col gap-3 px-5 py-4">
        <div className="flex items-center gap-2">
          <Network className="size-4 text-muted-foreground" />
          <h3 className="font-heading text-base font-semibold">
            IP e MAC desta máquina
          </h3>
          {lanIP && (
            <Badge variant="secondary" className="ml-auto">
              use na reserva DHCP do roteador
            </Badge>
          )}
        </div>
        <p className="-mt-3 text-sm text-muted-foreground">
          São estes valores que você registra no roteador: o{" "}
          <strong className="text-foreground">IP fixo</strong> da reserva DHCP
          e o <strong className="text-foreground">DNS primário</strong> = IP
          abaixo (a máquina desta tela).
        </p>
        {!lanIP ? (
          <p className="text-sm text-muted-foreground">
            O IP e o MAC aparecem quando o serviço FocusGuard está ativo e a
            máquina tem uma rota de rede. Confira com{" "}
            <code className="rounded bg-muted px-1">ipconfig</code> enquanto
            isso.
          </p>
        ) : (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <LanValue
              label="IP da máquina (LAN)"
              value={lanIP}
              onCopy={(t) => void copy(t, "IP")}
            />
            <LanValue
              label="MAC (reserva DHCP)"
              value={lanMAC || "—"}
              onCopy={(t) => void copy(t, "MAC")}
            />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// VendorScreenshot — captura de tela do painel do fabricante (aba ativa). A
// imagem vive em public/manuais/{id}.png; sem o arquivo (404) mostra um
// placeholder com a instrução de onde colocá-la — nunca quebra a aba.
function VendorScreenshot({ id, label }: { id: string; label: string }) {
  const [missing, setMissing] = useState(false);
  const src = `/manuais/${id}.png`;

  if (missing) {
    return (
      <div className="flex flex-col gap-1 rounded-lg border border-dashed border-border bg-muted/30 px-3 py-4 text-center">
        <span className="text-sm text-muted-foreground">
          Screenshot do painel ainda não disponível.
        </span>
        <span className="text-xs text-muted-foreground/80">
          Capture o painel de configuração do seu roteador{" "}
          <strong className="text-foreground">{label}</strong> (reserva DHCP +
          DNS) e salve como{" "}
          <code className="rounded bg-muted px-1">
            public/manuais/{id}.png
          </code>
          .
        </span>
      </div>
    );
  }

  return (
    <figure className="overflow-hidden rounded-lg border border-border">
      {/* Área de rolagem: capturas longas não empurram a página em telas
          menores — o rodapé com a legenda fica sempre visível. */}
      <div className="max-h-[28rem] overflow-y-auto">
        <img
          src={src}
          alt={`Painel de configuração ${label}: reserva DHCP e DNS`}
          loading="lazy"
          onError={() => setMissing(true)}
          className="h-auto w-full"
        />
      </div>
      <figcaption className="border-t border-border bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground">
        Painel {label} — referência visual (os menus exatos podem variar por
        modelo/firmware).
      </figcaption>
    </figure>
  );
}

// LanValue — célula de valor com botão de copiar (mono, truncado).
function LanValue({
  label,
  value,
  onCopy,
}: {
  label: string;
  value: string;
  onCopy: (t: string) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-xs text-muted-foreground">{label}</span>
        <span className="truncate font-mono text-sm font-semibold text-foreground">
          {value}
        </span>
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={() => onCopy(value)}
        aria-label={`Copiar ${label}`}
        title="Copiar"
      >
        <Copy />
      </Button>
    </div>
  );
}

// VENDORS — guias passo a passo por fabricante. `access` é o endereço do
// painel; `steps` são os caminhos de menu para reserva DHCP, DNS e IPv6;
// `note` (opcional) é um aviso específico do fabricante.
const VENDORS: {
  id: string;
  label: string;
  access: string;
  steps: ReactNode[];
  note?: string;
  manual?: { label: string; url: string };
}[] = [
  {
    id: "zte",
    label: "ZTE",
    access: "http://192.168.1.1 (ou 192.168.0.1)",
    manual: {
      label: "Manuais e suporte ZTE Devices",
      url: "https://www.ztedevices.com/en/support.html",
    },
    steps: [
      <>
        <strong className="text-foreground">Reserva DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">Rede → LAN → DHCP Server</code>{" "}
        → seção de reserva/static lease → MAC do PC → IP fixo (ex.:{" "}
        <code className="rounded bg-muted px-1">192.168.1.100</code>).
      </>,
      <>
        <strong className="text-foreground">DNS do DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">Rede → LAN → DHCP</code> →{" "}
        <strong className="text-foreground">DNS primário</strong> = IP fixo do
        PC; <strong className="text-foreground">secundário</strong> ={" "}
        <code className="rounded bg-muted px-1">1.1.1.1</code>.
      </>,
      <>
        <strong className="text-foreground">IPv6:</strong>{" "}
        <code className="rounded bg-muted px-1">Rede → IPv6 → DNS</code> →
        desligue o anúncio (RDNSS) ou aponte o DNS IPv6 para o PC — senão os
        aparelhos preferem o <code className="rounded bg-muted px-1">fe80::1</code>{" "}
        e burlam o sinkhole.
      </>,
    ],
  },
  {
    id: "tplink",
    label: "TP-Link",
    access: "http://tplinkwifi.net ou 192.168.0.1 / 192.168.1.1",
    manual: {
      label: "Emulador do painel (TP-Link)",
      url: "https://www.tp-link.com/us/support/emulator/",
    },
    steps: [
      <>
        <strong className="text-foreground">Reserva DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">
          Advanced → Network → DHCP Server → Address Reservation
        </code>{" "}
        → adicionar (MAC → IP fixo).
      </>,
      <>
        <strong className="text-foreground">DNS do DHCP:</strong> em{" "}
        <code className="rounded bg-muted px-1">
          Advanced → Network → DHCP Server
        </code>{" "}
        (ou <code className="rounded bg-muted px-1">Network → DNS</code>) →
        DNS primário = IP do PC; secundário ={" "}
        <code className="rounded bg-muted px-1">1.1.1.1</code>.
      </>,
      <>
        <strong className="text-foreground">IPv6:</strong>{" "}
        <code className="rounded bg-muted px-1">Advanced → Network → IPv6</code>{" "}
        → DNS — desligue o RDNSS ou aponte para o PC.
      </>,
    ],
  },
  {
    id: "huawei",
    label: "Huawei",
    access: "http://192.168.100.1 (fibra HG8xxx) ou 192.168.1.1",
    manual: {
      label: "Manuais de roteador (Huawei)",
      url: "https://consumer.huawei.com/en/support/router-manual/",
    },
    steps: [
      <>
        <strong className="text-foreground">Reserva DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">LAN → DHCP</code> → aba de
        reserva estática (MAC → IP fixo).
      </>,
      <>
        <strong className="text-foreground">DNS do DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">LAN → DHCP</code> → DNS
        primário = IP do PC; secundário ={" "}
        <code className="rounded bg-muted px-1">1.1.1.1</code>.
      </>,
      <>
        <strong className="text-foreground">IPv6:</strong> aba{" "}
        <code className="rounded bg-muted px-1">IPv6</code> → desligue o RDNSS
        ou aponte o DNS IPv6 para o PC.
      </>,
    ],
  },
  {
    id: "intelbras",
    label: "Intelbras",
    access: "http://192.168.1.1 (ou 10.0.0.1 em modelos RF)",
    manual: {
      label: "Ajuda e downloads (Intelbras)",
      url: "https://www.intelbras.com/pt-br/ajuda-download",
    },
    steps: [
      <>
        <strong className="text-foreground">Reserva DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">Rede → DHCP</code> → "Reserva
        de IP"/static lease → MAC → IP fixo.
      </>,
      <>
        <strong className="text-foreground">DNS do DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">Rede → DHCP</code> → campos de
        DNS → primário = IP do PC; secundário ={" "}
        <code className="rounded bg-muted px-1">1.1.1.1</code>.
      </>,
      <>
        <strong className="text-foreground">IPv6:</strong> seção IPv6 →
        desligue o anúncio de DNS (RDNSS) ou aponte para o PC.
      </>,
    ],
  },
  {
    id: "dlink",
    label: "D-Link",
    access: "http://192.168.0.1 ou http://dlinkrouter.local",
    manual: {
      label: "Suporte D-Link (manuais e firmware)",
      url: "https://support.dlink.com/",
    },
    steps: [
      <>
        <strong className="text-foreground">Reserva DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">
          Setup → Network Settings → DHCP Reservation
        </code>{" "}
        → adicionar (MAC → IP fixo).
      </>,
      <>
        <strong className="text-foreground">DNS do DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">Setup → Network Settings</code>{" "}
        → "Primary/Secondary DNS Server" → primário = IP do PC; secundário ={" "}
        <code className="rounded bg-muted px-1">1.1.1.1</code>.
      </>,
      <>
        <strong className="text-foreground">IPv6:</strong>{" "}
        <code className="rounded bg-muted px-1">Setup → IPv6</code> → desligue
        o RDNSS ou aponte o DNS para o PC.
      </>,
    ],
  },
  {
    id: "asus",
    label: "Asus",
    access: "http://router.asus.com ou 192.168.1.1",
    manual: {
      label: "Central de downloads (ASUS)",
      url: "https://www.asus.com/us/support/download-center/",
    },
    steps: [
      <>
        <strong className="text-foreground">Reserva DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">
          LAN → DHCP Server → Manual Assignment
        </code>{" "}
        → cliente da lista → IP fixo.
      </>,
      <>
        <strong className="text-foreground">DNS do DHCP:</strong>{" "}
        <code className="rounded bg-muted px-1">LAN → DHCP Server</code> →
        "DNS Server 1/2" → 1 = IP do PC; 2 ={" "}
        <code className="rounded bg-muted px-1">1.1.1.1</code>.
      </>,
      <>
        <strong className="text-foreground">IPv6:</strong> aba{" "}
        <code className="rounded bg-muted px-1">IPv6</code> → DNS — desligue o
        RDNSS ou aponte para o PC.
      </>,
    ],
  },
  {
    id: "outro",
    label: "Outro",
    access: "gateway padrão — ipconfig → 'Gateway Padrão'",
    steps: [
      <>
        Entre no painel e procure por{" "}
        <strong className="text-foreground">DHCP</strong> ou{" "}
        <strong className="text-foreground">Rede local (LAN)</strong>.
      </>,
      <>
        Crie a <strong className="text-foreground">reserva de endereço</strong>{" "}
        (MAC → IP fixo) e ponha o{" "}
        <strong className="text-foreground">DNS primário = IP do PC</strong>.
      </>,
      <>
        DNS secundário público (<code className="rounded bg-muted px-1">1.1.1.1</code>)
        e <strong className="text-foreground">desligue o RDNSS/DHCPv6 de DNS</strong>{" "}
        se houver opção.
      </>,
    ],
    note: "Sem reserva disponível? Use um IP fora da faixa do DHCP (ex.: 192.168.1.200) e configure-o fixo nas propriedades de rede do PC.",
  },
];
