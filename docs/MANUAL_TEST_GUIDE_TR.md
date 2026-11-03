# Community — arayüz kabul rehberi

Kalan iki senaryo (CE-05 ve CE-07), kullanıcının görevlendirmesiyle
29 Ekim 2026'da gerçek Chrome ve Edge üzerinde tamamlandı. Chrome bağlı
tarayıcı kontrolleriyle, Edge ise kullanıcının ayrıca onayladığı görünür
Playwright oturumuyla denendi. Tamamlanan senaryo talimatları bu rehberde tutulmaz.

Sonuçların ve kapsamın kalıcı kaydı
[manuel test matrisindedir](MANUAL_TEST_MATRIX.md#native-chrome-and-edge-completion-2026-10-29).
Sohbet sentetik sağlayıcıyla, bağlamsal algılama gerçek sabitlenmiş İngilizce
NER modeliyle denendi. 23 Ekim'deki ilk kontrolde bulunan üç eksik İngilizce kategori
etiketi düzeltildi ve yeniden doğrulandı; Türkçe çevirileri bekleyen kuyruktadır.

Chrome ve Edge'in yerel kabulü, önceki yedi senaryo ile bu iki senaryonun
kayıtları birlikte değerlendirilerek matriste yeşile alındı. Dokuz senaryonun
tamamı bu tur yeniden çalıştırılmış sayılmaz. Safari için gerçek macOS/Apple
cihazı gerekir. Hosted ve gerçek sağlayıcı kabulü bu yerel sonuçtan çıkarılamaz.

Daha önce kullanıcı tarafından kabul edilen C01–C08, C11–C12 ile C15/C20
adımları da 23 Ekim'deki bağımsız turda yeniden kontrol edildi. Çalıştırılan adımlar geçti;
%200 yakınlaştırma, Windows tema değişimi ve depolaması kapalı tarayıcı dalları
o tur yeniden doğrulanamadı. Ayrıntı ve kapsam sınırları
[ayrı kontrol kaydındadır](MANUAL_TEST_MATRIX.md#independent-ui-recheck-2026-10-23);
kullanıcının önceki kabulü korunur.

C10 ve C14 dahil yerelde tamamlanan API, model kesintisi ve gerçek süreç
kontrolleri [teknik kabul kaydına](MANUAL_TEST_MATRIX.md#technical-acceptance-run-2026-10-23)
işlendi. C26'nın yerel sürümler arası geri dönüş dahil operasyon kontrolleri
[tamamlandı](MANUAL_TEST_MATRIX.md#c26-local-operations-acceptance-2026-10-23).
Kullanıcı henüz hosting/proxy/APM/yedek altyapısı kurulmadığını belirtti;
bu ortamların kabulü yerel sonuçtan çıkarılmaz. C27'nin kalite, iptal/toparlanma
ve 15 dakikalık yük kontrolleri geçti; kullanıcı kabulüyle
[C27 yeşile alındı](MANUAL_TEST_MATRIX.md#c27-local-model-and-performance-acceptance-2026-10-23).
Kullanıcının kararıyla kısa NER istekleri için yeni iyileştirme hedefi **P99 <50 ms**.
Bu hedef zorunlu değildir; tek başına sürüm veya test kabulünü engellemez.
Ölçülen 40,4–49,3 ms P99, denenen ortamda bu hedefi karşılar.
Ölçümler [performans raporunda](PERFORMANCE_RESULTS.md#2026-10-23-local-default-configuration-reference), komutlar
[teknik doğrulama bölümünden](VERIFICATION.md#technical-manual-acceptance) takip edilir.

Önceki senaryoların kabulü korunur. Gemini'nin yerel senaryosu kullanıcının
görevlendirmesiyle Chrome ve Edge üzerinde, kalan gerçek Google dalı da
26 Ekim 2026'da sağlanan anahtarla tamamlandı. Sonuç ve kapsam
[C28 kaydındadır](MANUAL_TEST_MATRIX.md#gemini-acceptance--c28).
Tamamlanan Gemini talimatları kaldırıldı. Hosted kabulü ayrı ve beklemededir.
Rehber dosyasını silme kararı kullanıcıya aittir;
[örnek kit](MANUAL_TEST_EXAMPLES.md) kalıcı olarak korunur.

<a id="c29"></a>

## 1. Yeni istek kuyruğunu gözlemle ve iptal et (C29)

**Amaç:** Gerçek sıra konumunu, işleme geçişi ve iptalden sonra toparlanmayı görmek.
Bu yeni özellik önceki yeşil satırların kapsamına girmez. Yerel otomasyon geçti;
yeni manuel Chrome/Edge ve hosted kabulü ayrı bekler.

**Hazırlık ve örnek:** Geliştirici yerel pilotu (`prompt-shield-enterprise/docs/LOCAL_PILOT.md`)
açsın. İki Community sekmesinde OpenAI ve `fixture` modelini hazırla.
Birinci sekmede yalnız bu sentetik pilota ait `synthetic-delayed-credential`,
ikincide `synthetic-provider-credential` kullan. İki sekmeye de
[P11 metnini](MANUAL_TEST_EXAMPLES.md#shared-prompts) gir. Gerçek sağlayıcı
anahtarını bu yerel deneyde kullanma.

1. Birinci sekmede `Send protected message` düğmesine bas. Hemen ikinci
   sekmeden de gönder; ilk istek beş saniye beklerken ikinci istek sırada kalmalı.
2. İkinci sekmede `You are in the queue — position: 1` görünmeli. Slot alınca
   `Your request is being processed…` ve ardından cevap gelmeli. Yanıtta sentetik
   e-posta geri gelmeli; yer tutucu görünmemeli.
3. Aynı sırayı yeniden oluştur. İkinci sekmede `Cancel` kullan; bekleme mesajı
   kalkmalı. Geliştirici sağlayıcı sayacında bu iptal edilen iş için artış
   olmadığını kontrol etsin. Sonra yeni istek gönder; başarıyla tamamlanmalı.
4. Sıradayken `Forget key` ve sağlayıcı değiştirmeyi ayrıca dene. Eski cevap,
   anahtar, model veya konuşma geri gelmemeli. Anthropic `fixture` ve Gemini
   `gemini-fixture` ile de aynı akışı tekrarla.
5. Anahtarları unut, test sekmelerini kapat. Geliştirici kendi açtığı süreçleri
   kapatsın ve başlangıç ayarlarını geri getirsin.

**Tamamlandığında:** Test edilen tarayıcıda gerçek sıra, iptal ve yeni istekte
toparlanma gözlenmiştir. Tarayıcı sürümü/revizyon/alt dalları C29'a kaydet;
denenmeyen tarayıcı veya hosted yolu yeşile alma.

### Geliştirici ve hosting kontrolleri

Gerçek Redis'te iki gateway, dolu sıra, bekleme/işleme zaman aşımı, sahiplik
süresi, Redis arızası ve bağımsız çekirdekler için
[otomatik kanıtı](evidence/request-queue-20261030.json) incele.
`PUBLIC_DAILY_IP_LIMIT_ENABLED` ve Community'ye ait
`PUBLIC_IP_RATE_LIMIT_ENABLED` değişiklikleri kontrollü yeniden başlatma ister;
[sözleşme](REQUEST_QUEUE.md) esas alınır. Hosted ortam kurulunca gerçek ingress
üzerinde limitlerin açık/kapalı dalları, SSE buffering, bağlantı kopması ve
kapasite ayrıca denenecek. Bu eksik ortam yerel kontrolleri geçersiz kılmaz.
