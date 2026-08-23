# Screenshots dos painéis de roteador (tela Guia)

Cada aba de fabricante da tela **Guia** mostra uma captura de tela do painel
de configuração quando o arquivo correspondente existe nesta pasta. O nome do
arquivo é o **id do fabricante** (o mesmo das abas) com extensão `.png`:

| Arquivo        | Aba do Guia  | Status |
| -------------- | ------------ | ------ |
| `zte.png`      | ZTE          | ✅ (referência do painel) |
| `tplink.png`   | TP-Link      | ✅ painel Advanced > Network (DNS) |
| `huawei.png`   | Huawei       | ✅ painel EchoLife (EG8145V5) |
| `intelbras.png`| Intelbras    | ✅ painel GX-3000 (LAN IPv4) |
| `dlink.png`    | D-Link       | ⏳ pendente (placeholder) |
| `asus.png`     | Asus         | ✅ painel LAN > DHCP server (DNS) |

## Origem das capturas (atribuição)

Capturas de referência obtidas de documentação/suporte oficiais dos
fabricantes e de guias técnicos públicos (para fins de referência visual
neste projeto):

| Arquivo | Fonte |
| ------- | ----- |
| `tplink.png` | FAQ oficial TP-Link "How to Change DNS Settings on a TP-Link Router" (`tp-link.com/us/support/faq/1712/`) — painel Advanced > Network > Internet com DNS primário/secundário |
| `asus.png` | FAQ oficial ASUS "[LAN] How to disable the setting of using router LAN IP as DNS" (`asus.com/support/faq/1050080/`) — LAN > DHCP server > DNS and WINS |
| `intelbras.png` | Manual web oficial Intelbras (GX-3000) — `manuais.intelbras.com.br` (configuração LAN IPv4) |
| `zte.png` | Guia de configuração ZTE H3601P (operadora) — `help.mweb.co.za` / WebAfrica (assistente de conexão do painel) |
| `huawei.png` | Tópico técnico público sobre o painel Huawei EchoLife EG8145V5 (fórum Discourse pi-hole) |

## Como substituir por capturas do seu painel

1. Conecte-se ao painel do roteador real (ex.: `http://192.168.1.1`) com
   usuário/senha de administrador.
2. Navegue até a página que mostra os menus citados no guia da aba
   (reserva DHCP + DNS). Prefira a visão que mostra **ao mesmo tempo** a
   reserva de endereço e os campos de DNS.
3. Capture a tela inteira (sem recortar) com `Win+Shift+S` (Windows) ou
   `PrtSc` e salve como PNG na pasta `focusguard-ui/public/manuais/` com o
   nome da tabela acima (sobrescreve a referência).
4. Se o painel tiver a opção de DNS **IPv6/RDNSS** na mesma tela, melhor
   ainda — mostra o passo do `fe80::1`.

## Como o app usa

A tela Guia renderiza `<img src="/manuais/{id}.png">` na aba ativa (com
`max-h` e rolagem em telas menores). Se o arquivo não existir (404), um
placeholder com instruções aparece no lugar — sem quebrar a tela. É o caso
atual da aba **D-Link**: captura oficial indisponível em fonte viva.
