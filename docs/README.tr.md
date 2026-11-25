# patrolbox

**Kendi sunucunu işletenler için ayar gerektirmeyen bir bekçi.** Tek dosya; veritabanı yok, web paneli yok, sürekli çalışan bir servis yok.

patrolbox, Caddy, nginx ve Docker ayarlarını okuyarak sunucunda barındırdığın siteleri kendisi bulur, kontrol eder ve bir şey bozulduğunda sana mesaj atar. Sorun düzeldiğinde de haber verir.

🌍 [English](../README.md)

```console
$ patrolbox --lang tr
Yapılandırma: yok (yerleşik varsayılanlar)

KRİTİK  Sertifika   shop.example.com  yalnızca 3 gün sonra sona erecek (2026-01-04), veren: R11
UYARI   Disk        /                 dolmak üzere: %84 dolu, 12.3 GB boş
TAMAM   Yedek       database          son yedek db-2026-01-01.sql.gz, 5h önce alınmış, 48.2 MB
TAMAM   Konteyner   postgres          çalışıyor, 12d süredir ayakta
TAMAM   Web sitesi  example.com       HTTP 200, 142 ms

3 tamam, 1 uyarı, 1 kritik, 0 bilinmiyor
```

## Neleri kontrol eder

| Kontrol | Yakaladığı sorun |
|---|---|
| **Web sitesi** | Site kapalı, hata kodu dönüyor, yavaş yanıt veriyor, beklenen metin sayfada yok |
| **Sertifika** | TLS sertifikasının süresi doluyor, dolmuş ya da alan adıyla uyuşmuyor |
| **Alan adı** | Alan adı kaydının süresi bitmek üzere (RDAP üzerinden) |
| **Konteyner** | Docker konteyneri sürekli yeniden başlıyor, durmuş, sağlıksız ya da bellek yetmediği için öldürülmüş |
| **Disk** | Diskte yer ya da inode bitiyor |
| **Yedek** | Son yedek dosyası çok eski, boş ya da şüpheli derecede küçük |

Siteler şuralardan bulunur:

- **Caddy**: Caddyfile dosyaları (`import` edilenler dahil) ve yönetim API'si
- **nginx**: TLS kullanan her `server` bloğunun `server_name` değeri (`include` takip edilir)
- **Docker**: çalışan konteynerlerdeki caddy-docker-proxy ve Traefik etiketleri

Ne bulduğunu görmek için `patrolbox discover` komutunu çalıştır. Bulamadığı siteleri ayar dosyasında `sites:` altına ekleyebilirsin.

## Kurulum

```bash
curl -fsSL https://raw.githubusercontent.com/ferhalemre/patrolbox/main/install.sh | sh
```

Betik, sistemine uygun son sürümü `/usr/local/bin` altına indirir ve sağlama toplamını doğrular. Kaynaktan derlemek istersen:

```bash
go install github.com/ferhalemre/patrolbox/cmd/patrolbox@latest
```

## Kullanım

```bash
patrolbox              # tüm kontrolleri bir kez çalıştırır
patrolbox discover     # bulduğu siteleri listeler
patrolbox init         # her seçeneği açıklayan patrolbox.yml dosyasını yazar
patrolbox test         # deneme bildirimi gönderir
```

Sonra cron'a ekle. Beş dakikada bir çalıştırmak iyi bir başlangıçtır:

```cron
*/5 * * * * /usr/local/bin/patrolbox --quiet --config /etc/patrolbox/patrolbox.yml
```

patrolbox her kontrolün önceki sonucunu hatırlar. Bu sayede **bir şey bozulduğunda bir mesaj, düzeldiğinde bir mesaj** alırsın; beş dakikada bir aynı uyarı gelmez. Hâlâ süren sorunların hatırlatılmasını istersen `notify.repeat` değerini ayarla.

Seçenekler: `--config DOSYA`, `--lang KOD`, `--json`, `--quiet` (yalnızca sorunlar), `--no-notify`.

Çıkış kodları izleme araçlarının ortak kuralına uyar: `0` tamam, `1` uyarı, `2` kritik, `3` bilinmiyor. Böylece patrolbox başka araçların içinde bir kontrol olarak da kullanılabilir.

## Bildirimler

Telegram, Slack, Discord, ntfy, e-posta (SMTP) ve genel bir JSON webhook desteklenir. İstediğin kadarını birlikte kullanabilirsin:

```yaml
notify:
  repeat: 24h
  telegram:
    bot_token: "${TELEGRAM_TOKEN}"
    chat_id: "123456789"
```

`${AD}` biçimindeki değerler ortam değişkenlerinden okunur; böylece gizli bilgileri dosyaya yazmak zorunda kalmazsın.

## Ayarlar

Hiçbir ayar zorunlu değildir; ayar dosyası yoksa patrolbox makul varsayılanlarla çalışır. `patrolbox init`, her seçeneğin açıklandığı bir dosya yazar. Kısa bir örnek:

```yaml
lang: tr
exclude: ["staging.*"]

tls:
  warn_days: 14
  crit_days: 5

backups:
  - name: veritabani
    path: /var/backups/db/*.sql.gz
    max_age: 26h

docker:
  require: [postgres, "app-*"]
```

Ayar dosyası önce `./patrolbox.yml`, sonra `/etc/patrolbox/patrolbox.yml` yolunda aranır.

## Diller

Çıktı ve bildirimler İngilizce, Türkçe, Almanca, İspanyolca, Fransızca, Portekizce, Rusça ve Çince olarak kullanılabilir. Dil sistemden (`LANG`) alınır; ayar dosyasındaki `lang:` ya da `--lang` ile değiştirilebilir.

Yeni bir dil eklemek tek bir klasörlük JSON işidir: `internal/i18n/locales/en` klasörünü kendi dil kodunla kopyala ve değerleri çevir. `go test ./...` eksik anahtar ya da yer tutucu kalmadığını denetler.

## Gereksinimler

Konteyner ve disk kontrolleri Linux'ta çalışır. Web sitesi, sertifika, alan adı ve yedek kontrolleri macOS ve Windows'ta da çalışır. Docker soketini okumak için root yetkisi ya da `docker` grubuna üyelik gerekir.

## Lisans

[MIT](../LICENSE)
