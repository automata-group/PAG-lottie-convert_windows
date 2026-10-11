# إشعارات وتراخيص الطرف الثالث 📜
# Third-Party Notices & Open Source Licenses

---

# 📖 إشعارات التراخيص وحقوق الملكية الفكرية (باللغة العربية)

تتضمن هذه الوثيقة كافة الإشعارات القانونية وحقوق النشر والتراخيص مفتوحة المصدر للمكونات البرمجية المدمجة أو المستخدمة ضمن مشروع **lottie2pag**.

---

## 1. ترخيص المشروع الأساسي (Project License)

مشروع **lottie2pag** مرخص بموجب ترخيص **MIT** المفتوح المصدر (راجع ملف [LICENSE](LICENSE)).
يمنح هذا الترخيص إذناً صريحاً ومجانياً لأي شخص للحصول على نسخة من هذا البرنامج والتعامل معه دون قيود، بما في ذلك حقوق الاستخدام والنسخ والتعديل والدمج والنشر والتوزيع والترخيص من الباطن والبيع، بشرط إدراج إشعار حقوق النشر وإشعار الإذن في جميع النسخ.

---

## 2. إخلاء المسؤولية القانونية واستقلالية المشروع (Disclaimer & Independence)

* **مشروع مستقل من المجتمع البرمجي**: أداة **lottie2pag** هي مشروع مجتمعي حر ومستقل تم تطويره لخدمة المطورين.
* **عدم التبعية لشركة Tencent**: هذا المشروع **ليس** تابعاً لشركة **Tencent Holdings Limited**، ولم يتم تطويره أو رعايته أو اعتماده أو توثيقه رسمياً من قبل شركة Tencent أو الفريق الرسمي لمكتبة Tencent PAG.
* **استخدام الأسماء والعلامات التجارية**: كلمتا "PAG" و"Tencent" علامتان تجاريتان مملوكتان لأصحابهما القانونيين. إن أي إشارة إلى PAG أو Tencent أو libpag في هذا المشروع تندرج حصرياً تحت بند **الاستخدام العادل الاسمي (Nominative Fair Use)** لوصف توافق صيغة الملفات الثنائية والتحقق البرمجي مع محرك العرض الرسمي.

---

## 3. مكونات الطرف الثالث والمكتبات المدمجة (Third-Party Components)

### أ. مكتبة lottie-web (مدمجة عبر `go:embed`)
* **المكون**: `assets/lottie.min.js`
* **المؤلف**: Hernan Torrisi & شركة Airbnb
* **المستودع**: https://github.com/airbnb/lottie-web
* **الترخيص**: ترخيص MIT
* **الوظيفة**: مسؤولة عن تفسير رسوم Lottie JSON وبنائها على سطح Canvas داخل بيئة المتصفح الخفي لاستخراج الإطارات بدقة متناهية.

### ب. حزمة klauspost/compress (اعتمادية Go Module)
* **المكون**: `pkg/compress/zstd.go`
* **المؤلف**: Klaus Post ومساهمو المشروع
* **المستودع**: https://github.com/klauspost/compress
* **الترخيص**: ترخيص BSD 3-Clause
* **الوظيفة**: محرك Zstandard فائق السرعة والكفاءة لتطبيق ضغط الإنتروبيا (Finite State Entropy / tANS) على الحاوية الثنائية لملفات PAG.

### ج. مواصفات Tencent libpag ومحرك الفحص libpag.wasm
* **المكون**: مواصفات صيغة ملفات PAG وحاوية الفحص `libpag.wasm` في خيار `-verify`
* **المؤلف**: THL A29 Limited (إحدى شركات Tencent)
* **المستودع**: https://github.com/Tencent/libpag
* **الترخيص**: ترخيص Apache License 2.0
* **الوظيفة**: المرجع القياسي لهيكل الوسوم الثنائية (Binary Tags) واختبار التحقق من صحة الملفات الناتجة وتوافقها مع المحرك الرسمي.

---
---

# 🇬🇧 English Documentation & Official License Texts

This document contains open-source software notices and license attributions for components bundled, embedded, or utilized by **lottie2pag**.

---

## 1. Project License (MIT)

**lottie2pag** is licensed under the [MIT License](LICENSE).

```text
MIT License

Copyright (c) 2026 automata-group

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## 2. Independent Project Disclaimer

**lottie2pag** is an independent, community-developed open-source converter tool.
It is **not** developed, sponsored, endorsed, or certified by **Tencent Holdings Limited** or the official Tencent PAG team.
"PAG" and "Tencent" are trademarks of their respective owners. References to PAG, Tencent, and libpag are strictly nominative fair use to describe file format compatibility and runtime verification.

---

## 3. Third-Party Dependencies and Embedded Components

### A. lottie-web (Embedded via `go:embed`)
* **Component**: `assets/lottie.min.js`
* **Author**: Hernan Torrisi & Airbnb
* **Source**: https://github.com/airbnb/lottie-web
* **License**: MIT License

```text
The MIT License (MIT)

Copyright (c) 2015 Hernan Torrisi

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

### B. klauspost/compress (Go Module Dependency)
* **Component**: `pkg/compress/zstd.go` (Zstandard compression engine)
* **Author**: Klaus Post and contributors
* **Source**: https://github.com/klauspost/compress
* **License**: BSD 3-Clause License

```text
Copyright (c) 2019 Klaus Post. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Klaus Post nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

---

### C. Tencent libpag (Format Reference & Verification Target)
* **Component**: PAG Format Specification & libpag.wasm verification harness (`-verify`)
* **Author**: Tencent Holdings Limited
* **Source**: https://github.com/Tencent/libpag
* **License**: Apache License, Version 2.0

```text
Copyright (C) 2019 THL A29 Limited, a Tencent company. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```
