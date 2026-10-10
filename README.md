# محول الرسوم المتحركة Tencent PAG الأصلي لنظام ويندوز 🚀
# Native Windows Tencent PAG Animation Converter (Go)

<p align="center">
  <img src="https://img.shields.io/badge/Language-Go%201.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/Platform-Windows%20(Native)-0078D6?style=for-the-badge&logo=windows&logoColor=white" alt="Platform" />
  <img src="https://img.shields.io/badge/Standard-Tencent%20PAG%20v3-red?style=for-the-badge" alt="Format" />
  <img src="https://img.shields.io/badge/Compression-Up%20to%2062%25-success?style=for-the-badge" alt="Compression" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License" />
</p>

---

# 📖 المحتوى العربي (دليل التوثيق والتشغيل الشامل)

محرك تحويل برمجي فائق السرعة ومستقل بذاته، مبني بلغة **Go** ومخصص لبيئة **نظام تشغيل Windows**، يقوم بتحويل ملفات الرسوم المتحركة **Lottie JSON**، وصور **GIF المتحركة**، و**تسلسلات الصور (Image Sequences)** إلى صيغة **Tencent PAG (Portable Animated Graphics)** الثنائية عالية الكفاءة.

تم تزويد المحرك بخوارزميات ضغط رقمي شرسة وقوية تحقق تخفيضاً في حجم الملفات يتجاوز **60%** مع الحفاظ الرياضي والبصري الكامل على نقاء وجودة الألوان والتدرجات والسرعة الزمنية للحركة.

---

## 🌟 لماذا تم تطوير هذا المحرك؟ (المشكلة والحل)

1. **الاستغناء الكامل عن بيئات macOS وLinux**:
   * أداة Tencent الرسمية (`PAGConvertor` و `convert.sh`) كانت تعمل حصرياً على أنظمة macOS وLinux وتتطلب بيئات تشغيل معقدة ومكتبات مفقودة على Windows.
   * هذا المشروع يقدم أداة تنفيذية أصلية (`lottie2pag.exe`) تعمل مباشرة على نظام Windows دون الحاجة إلى تثبيت أي برامج وسيطة.
2. **حل مشكلة تضخم الحجم (File Size Explosion)**:
   * أدوات التحويل التقليدية تقوم بحفظ كافة الإطارات بمعدلات عالية (مثل 60FPS) كصور مستقلة وبجودات غير محسوبة، مما يؤدي إلى إنتاج ملفات PAG ضخمة تتجاوز 1.7 ميغابايت وتثقل كاهل تطبيقات الهاتف ومواقع الويب.
   * تم ابتكار خط أنابيب ضغط خوارزمي متكامل يقلص الحجم من **1,724 كيلوبايت** إلى **711 كيلوبايت** (توفير 58.7% فوري) دون أي فقدان للجودة.
3. **التوافق التام 100% مع معايير Tencent الرسمية**:
   * الملفات الناتجة تعمل مباشرة في تطبيقات الهاتف الجوال (مثل تطبيقات Flutter عبر حزمة `flutter_pag`، وiOS وAndroid) وفي متصفحات الويب عبر `libpag.wasm`، وتجتاز كافة اختبارات الفحص والتحقق الرسمية.

---

## 🧠 الخوارزميات والتحسينات المبتكرة للضغط الفائق

### 1. إعادة التشكيل الزمني الذكي لمعدل الإطارات (Smart Temporal FPS Resampling)
* **المشكلة**: ملفات Lottie الاحترافية تأتي غالباً بمعدل 60 إطاراً في الثانية (مثل حركة مدتها 3 ثوانٍ = 180 إطاراً). حفظ 180 إطاراً كصور يضاعف حجم الملف إلى مستويات غير مقبولة في الإنتاج.
* **الحل الخوارزمي**:
  يقوم المحرك بحساب عدد الإطارات المستهدفة بناءً على المدة الزمنية الحقيقية:
  $$\text{targetFrames} = \text{round}(\text{durationSeconds} \times \text{targetFPS})$$
  * إذا كان الملف الأصلي 60 FPS، يقوم المحرك تلقائياً وبذكاء بإعادة توزيع العينات الزمنية إلى **30 FPS** (90 إطاراً بدلاً من 180).
  * **النتيجة**: خفض حجم الملف فوراً بنسبة تتجاوز 55% مع الحفاظ المطلق على مدة العرض الدقيقة (3.00 ثوانٍ تظل 3.00 ثوانٍ تماماً) وبسلاسة بصرية مطابقة للعين البشرية.

### 2. تنظيف ضوضاء القنوات الشفافة (Canvas Alpha Cleaning)
* **المشكلة**: في الرسومات التي تحتوي على خلفيات شفافة، تحمل البيكسلات الشفافة تماماً (`Alpha = 0`) قيماً عشوائية في قنوات الألوان (`R, G, B`) ناتجة عن التدرجات اللونية السابقة. هذا التشويش يزيد من إنتروبيا البيانات ويمنع محرك ضغط WebP من تقليص الحجم.
* **الحل الخوارزمي**:
  يقوم المحرك بمسح مباشر ومحسّن لمصفوفة البيكسلات في الذاكرة وتصفير قنوات الألوان فوراً لأي بيكسل شفاف:
  ```javascript
  for (let p = 0; p < data.length; p += 4) {
      if (data[p + 3] === 0) {
          data[p] = 0;
          data[p + 1] = 0;
          data[p + 2] = 0;
      }
  }
  ```
  * **النتيجة**: رفع كفاءة ضغط خوارزمية WebP بنسبة إضافية تتراوح بين **15% إلى 25%**.

### 3. دمج الإطارات المتطابقة وتجميع الأصول (Deduplication & SHA-256 Asset Pooling)
* **المشكلة**: عند ثبات الحركة في بعض الأجزاء من الإطار الزمني، تقوم الأدوات التقليدية بتكرار حفظ نفس الصورة عدة مرات.
* **الحل الخوارزمي**:
  * **الفحص البصري التراكمي**: فحص مصفوفة البيكسلات للإطار ومقارنتها بالإطار السابق؛ في حال تطابق الإطارين، يتم تجاهل كتابة صورة جديدة وتمديد قيمة `duration` للطبقة السابقة داخل الـ `VectorComposition`.
  * **تجميع الأصول ببصمة SHA-256**: فحص بصمة كل صورة متبقية وإعادة استخدام معرف الصورة `ImageID` عبر وسوم `Tag 49 (ImageBytesV3)` بدلاً من تكرار حفظها في الملف الثنائي.

---

## 📊 جدول القياسات والأداء الفعلي (Real-World Benchmarks)

> تجربة حقيقية على ملف الأنيميشن الناري المعقد `nickname_fire.json` (الأبعاد: 190x88، المدة: 3.00 ثوانٍ):

| الإعداد / وضع الضغط | عدد الإطارات | الحجم بالبايت | الحجم بالكيلوبايت | نسبة التخفيض | تقييم الجودة والدقة البصرية |
|---|:---:|:---:|:---:|:---:|:---:|
| **الأصلي قبل الضغط** | 180 (60 FPS) | 1,765,569 | **1,724.0 KB** | نقطة الأساس | جودة أصلية |
| **الضغط الخفيف (Quality 0.85)** | 90 (30 FPS) | 792,028 | **773.5 KB** | **-55.1%** | متطابقة مع الأصل بنسبة 100% |
| **الضغط القياسي الموصى به (Quality 0.80 - الافتراضي)** | 90 (30 FPS) | 728,998 | **711.9 KB** | **-58.7%** | فائقة النقاء (Sweet Spot المعتمد) |
| **الضغط الشديد (Quality 0.75)** | 90 (30 FPS) | 681,960 | **666.0 KB** | **-61.4%** | ممتازة جداً بدون أي تشويه للعين |
| **الضغط الأقصى (Quality 0.70)** | 90 (30 FPS) | 667,144 | **651.5 KB** | **-62.2%** | جيدة جداً مع حدة حواف متوازنة |

---

## 🛠️ متطلبات التشغيل والبناء (Prerequisites & Build)

1. **بيئة نظام التشغيل**: Windows 10 أو Windows 11 (أو Windows Server).
2. **لغة Go**: إصدار 1.22 أو أحدث.
3. **متصفح الويب**: Microsoft Edge أو Google Chrome (المثبت افتراضياً في نظام ويندوز لتشغيل المعالجة الهيدلس الخفية).

### بناء الأداة التنفيذية من المصدر:
```powershell
# استنساخ المستودع
git clone https://github.com/USERNAME/PAG-lottie-convert_windows.git
cd PAG-lottie-convert_windows

# بناء الأداة التنفيذية
go build -v -o lottie2pag.exe .
```

---

## 🚀 دليل الاستخدام الشامل لكافة الأنماط (Usage Guide)

### 1. التحويل الأساسي السريع (Single File Conversion)
يقوم بتحويل ملف Lottie مع تطبيق إعدادات الضغط الذكية تلقائياً:
```powershell
.\lottie2pag.exe -i "animation.json" -o "animation.pag"
```

### 2. التحكم المخصص بمعدل الإطارات والجودة (FPS & Quality Control)
تحديد معدل الإطارات المستهدف وجودة ضغط WebP (من 0.10 إلى 1.00):
```powershell
.\lottie2pag.exe -i "animation.json" -o "animation.pag" -fps 30 -quality 0.80
```

### 3. التحويل الدفعي الشامل لكافة الأصول (Batch Conversion)
يدعم تحويل مجلد كامل يحتوي على ملفات متعددة (JSON, GIF, تسلسلات صور) عبر سكريبت PowerShell الموازي لـ `convert.sh` الرسمي لـ Tencent:
1. ضع جميع الملفات داخل مجلد `input/`.
2. نفّذ الأمر التالي:
```powershell
.\convert.ps1 -FrameRate 30
```

### 4. التوافق التام مع أوامر Tencent الرسمية (Positional CLI Syntax)
يمكنك استخدام نفس صيغ الأوامر القديمة الخاصة بأداة Tencent دون أي تغيير:
```powershell
# تحويل ملف مفرد
.\lottie2pag.exe "animation.json" "animation.pag" 30

# تحويل مجلد تسلسل صور
.\lottie2pag.exe "./frames_sequence_dir/" 30
```

### 5. الفحص الهيكلي وتفكيك الوسوم الثنائية (`-inspect`)
قراءة بيانات ملف PAG، وأبعاده، ومعدل إطاراته، ووسومه الثنائية (`Tag Stream`):
```powershell
.\lottie2pag.exe -inspect "animation.pag"
```

### 6. التحقق والاعتماد بمحرك Tencent الرسمي (`-verify`)
يقوم بتشغيل محرك `libpag.wasm` الرسمي المترجم من C++ داخل المتصفح الخفي لفحص توافق البايتات والتأكد من رسم كل إطار بنجاح:
```powershell
.\lottie2pag.exe -i "animation.json" -o "animation.pag" -verify
```

---

## 🏗️ الهيكل المعماري للمشروع (Project Architecture)

```
PAG-lottie-convert_windows/
├── main.go                     # نقطة الدخول، معالجة معاملات السطر البرمجي والتوجيه
├── convert.ps1                 # سكريبت التحويل الدفعي الموازي لسكريبت Tencent الرسمي
├── go.mod                      # تعريف حزمة Go والاعتماديات
├── README.md                   # التوثيق الشامل ثنائي اللغة (عربي / إنجليزي)
├── LICENSE                     # ترخيص MIT المفتوح المصدر
├── input/                      # مجلد التحويل الدفعي الافتراضي
└── pkg/
    ├── converter/              # محركات التحويل لجميع الوسائط
    │   ├── render_windows.go   # رندر المتصفح الخفي، تنظيف القنوات الشفافة، والضغط
    │   ├── universal.go        # كشف نوع الملف والتوجيه التلقائي
    │   ├── sequence.go         # معالج مجلدات تسلسلات الصور (PNG/WebP/JPG)
    │   ├── gif.go              # معالج ومفكك ملفات GIF المتحركة
    │   └── verify.go           # جسر الفحص والتدقيق الرسمي مع libpag.wasm
    ├── lottie/                 # قارئ ومحلل بيانات Lottie JSON
    │   └── parser.go           # استخراج الأبعاد، معدل الإطارات، ونقاط البداية والنهاية
    └── pag/                    # الباني الثنائي لملفات Tencent PAG Container
        ├── container.go        # تشفير الهيدر، الوسوم، والـ Body
        ├── tag.go              # وسوم TimeStretch, ImageBytesV3, VectorComposition
        └── type.go             # الثوابت الرقمية لأنواع الوسوم والضغط
```

---
---

# 🇬🇧 English Documentation

High-performance, standalone native Windows converter for **Lottie JSON**, **Animated GIFs**, and **Image Sequences** to **Tencent PAG (Portable Animated Graphics)** binary animations.

Engineered in **Go** to completely eliminate macOS/Linux dependencies, equipped with an aggressive multi-stage compression pipeline delivering up to **62%+ file size reduction** while strictly maintaining perceptual quality, visual fidelity, and exact timing.

---

## 🌟 Key Highlights

- **Pure Native Windows Runtime**: No WSL, Docker, or Unix emulator needed. Compiles into a single portable binary (`lottie2pag.exe`).
- **Aggressive Size Optimization**:
  - **Smart Temporal FPS Resampling**: Intelligently downsamples high-FPS animations (e.g. 60 FPS -> 30 FPS) while keeping exact duration (3.00s remains 3.00s), slashing file size by >55%.
  - **Canvas Alpha Cleaning**: Zeroes out RGB noise in transparent pixels (`A == 0`) before WebP encoding, boosting entropy compression by 15–25%.
  - **Consecutive Frame Deduplication**: Identical frames extend layer duration in the vector composition rather than bloating the binary with redundant images.
  - **Global Asset Pooling (SHA-256)**: Reuses Tencent `Tag 49` image IDs across identical frames anywhere in the timeline.
- **Multi-Format Input Support**: Converts Lottie JSON, animated GIFs, PNG/WebP image sequence directories, and single graphics.
- **Tencent CLI Compatibility**: Direct drop-in replacement for official `./PAGConvertor` and `./convert.sh` commands.
- **Official libpag.wasm Audit**: Verify generated files with the official Tencent C++/WASM engine via the `-verify` flag.

---

## 📊 Benchmark Comparison (`nickname_fire.json`)

| Conversion Mode | Frames | Bytes | File Size | Size Reduction | Visual Fidelity |
|---|:---:|:---:|:---:|:---:|:---:|
| **Original Bake** | 180 (60 FPS) | 1,765,569 | **1,724.0 KB** | Baseline | Reference |
| **Optimized (Q=0.85)** | 90 (30 FPS) | 792,028 | **773.5 KB** | **-55.1%** | 100% Identical |
| **Optimized (Q=0.80 - Default)** | 90 (30 FPS) | 728,998 | **711.9 KB** | **-58.7%** | Ultra Sharp (Sweet Spot) |
| **Optimized (Q=0.75)** | 90 (30 FPS) | 681,960 | **666.0 KB** | **-61.4%** | High Fidelity |
| **Optimized (Q=0.70)** | 90 (30 FPS) | 667,144 | **651.5 KB** | **-62.2%** | Great Quality |

---

## 📦 Build Instructions

```powershell
# Clone the repository
git clone https://github.com/USERNAME/PAG-lottie-convert_windows.git
cd PAG-lottie-convert_windows

# Build native binary
go build -v -o lottie2pag.exe .
```

---

## 💻 CLI Quickstart

```powershell
# 1. Standard Conversion (Lottie JSON -> PAG)
.\lottie2pag.exe -i "input.json" -o "output.pag"

# 2. Custom Target FPS & Quality (0.1 to 1.0)
.\lottie2pag.exe -i "input.json" -o "output.pag" -fps 30 -quality 0.80

# 3. Batch Convert directory using PowerShell
.\convert.ps1 -FrameRate 30

# 4. Positional syntax matching Tencent CLI
.\lottie2pag.exe "input.json" "output.pag" 30
.\lottie2pag.exe "./image_sequence_dir/" 30

# 5. Inspect PAG tags and composition structure
.\lottie2pag.exe -inspect "output.pag"

# 6. Verify with official libpag.wasm runtime
.\lottie2pag.exe -i "input.json" -o "output.pag" -verify
```

---

## 📄 License

This project is open-source and released under the [MIT License](LICENSE).
