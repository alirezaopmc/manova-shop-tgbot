# Motion storyboard: shop_manovabot promo (draft)

Status: draft for owner review. Research and writing only. Nothing is built, no repository is created, and nothing is pushed.

## 1. Brief and format

- Bot: @shop_manovabot, a Telegram shop for Hetzner cloud servers. Support: @manova_support.
- Approved slogan: "اولین خرید بدون کارمزد". It must not be shown as a general "no fee" claim (see approval 1).
- Format: 1080 × 1920 (9:16), 30 fps, MP4 (H.264). Length: 30 s (default, to be confirmed).
- Tone: calm and trustworthy. No flashing, no shake, slow and predictable transitions.
- Audience: people who know servers. High-level copy, and the message is "ordering is easy".
- Walkthrough shown: plan, then Ubuntu or Debian and a version, then order summary, then confirm. Payment is skipped.
- Persian copy is right-to-left. Latin terms (Hetzner, CX23, Ubuntu, handles) sit in LTR isolates. Digits are Latin, which matches the design default (`docs/shop-system-design.md`, sections 15.4 and 26.2).

## 2. Palette

Sampled from `media/brand/manova-logo.png` (640 × 640). Method: k-means over the non-white pixels. The values are cluster centres, so they may differ by a few units from the original source file, which was not provided.

| Role | Hex | Source | Use |
|---|---|---|---|
| Brand Light | #B269F9 | Sampled (left face) | Decorative only. Contrast on white is 3.3:1, so not for text |
| Brand Mid | #A85BF5 | Sampled | Gradients, highlights |
| Brand Core | #853ADE | Sampled | Primary buttons, user bubbles |
| Brand Deep | #7930D6 | Sampled (right face) | Headline accent, text on light. White on this is 6.6:1 |
| Brand Highlight | #CD97F9 | Sampled | Progress lines, selected button outline |
| Ink | #1E1433 | Derived, not in logo | Body text. 15.9:1 on Canvas |
| Canvas | #F6F3FC | Derived, not in logo | Chat background and intro background |
| White | #FFFFFF | Logo background | Bot bubbles, card surfaces |

## 3. Typography

- Persian (all headlines and body): Vazirmatn, SIL Open Font License 1.1 (verified from the project's OFL.txt). Headlines Bold 700 at about 96 px, body Regular 400 at 40 px, chat text 38 px, button labels Medium 500 at 34 px, fine print 30 px. Nothing smaller than 30 px.
- Latin runs (CX23, Ubuntu, handles): Vazirmatn if its Latin glyphs render cleanly in a test render (UNVERIFIED), otherwise a Latin sans with an open licence chosen in that test.
- Logo: use the supplied PNG as is. No recolouring.
- Safe zones: keep text out of the top 250 px and the bottom 300 px (a proposed margin for platform overlays, not verified against any platform spec).

## 4. Tool comparison

| | Remotion | Open Design (HyperFrames engine) |
|---|---|---|
| Licence | Free for individuals and teams of up to 3 people. Above that, Remotion for Creators costs $25 per seat per month for each person who writes Remotion code. Verified from remotion.dev licence FAQ and pricing excerpts. Your headcount is not known, so the cost for you is UNVERIFIED | Open Design is Apache-2.0. HyperFrames is Apache-2.0 per the Open Design README. Verified from GitHub |
| Authoring | React and TypeScript. Motion is code, so a re-render from the same source gives the same file | HTML, CSS and GSAP. Open Design's agent writes the composition. HyperFrames describes its render as deterministic (headless Chrome and FFmpeg). Verified from the repository docs |
| Persian font | Local font via `loadFont()` with a woff2 in `public/`. Verified from Remotion docs. Persian shaping and RTL in a real render: UNVERIFIED (not tested) | The HyperFrames bundled font list is Latin-focused (only Noto Sans JP for CJK). Vazirmatn would need a local `@font-face`, which the compiler embeds. Persian render: UNVERIFIED |
| Platform | Standard Node-based toolchain. Not tested on this machine | Latest official release has no prebuilt Linux desktop app. Running from source needs Node 24 and pnpm. Verified from the README |
| Maturity | Established library | Motion shipped in Open Design 0.25.0 (2026-10-15). New |
| Data | Local rendering | Cloud model requests go to the chosen provider. Verified from README |

**Recommendation: Remotion.** Reasons:
1. The storyboard becomes source code in your own project, so each change is a known, repeatable edit. Open Design's motion output depends on an agent rewriting the composition, which makes later edits harder to control.
2. Vazirmatn loading and RTL layout are standard, documented web-font and CSS work. Open Design's Persian support is not documented.
3. Open Design's motion feature is one release old, and Linux runs from source.
4. Cost is zero if the team is three people or fewer. If it is larger, the seat cost is $25 per month for each person who writes the code.

Fallback: HyperFrames under Open Design, if the team is larger than three and the zero-licence cost matters more than maturity.

## 5. Scenes (30 s)

Visual notes assume the Telegram-style chat is a generic chat layout (bot bubbles left, user bubbles right, RTL text inside). It does not use the Telegram logo or its trademark assets.

| # | Time | Visual | On-screen Persian text (exact) |
|---|---|---|---|
| 1 | 0:00–0:03 | MN cube assembles on Canvas, then settles with a soft light sweep | none |
| 2 | 0:03–0:06 | Slogan fades in under the logo | Headline: اولین خرید بدون کارمزد |
| 3 | 0:06–0:09 | Phone frame slides up. Header shows the MN avatar, "Manova" and @shop_manovabot. Bot message appears, then one button | Bot: سلام! به فروشگاه سرورهای Hetzner خوش آمدید. Button: سرورهای ابری |
| 4 | 0:09–0:13 | Tap on "سرورهای ابری". Bot shows plan buttons. Tap on CX23 (highlight ring) | User: سرورهای ابری. Bot: یک پلن را انتخاب کنید: Buttons: CX23 and one other live-catalog plan name (to be confirmed) |
| 5 | 0:13–0:18 | Tap on Ubuntu, then tap on Ubuntu 24.04 in the version list (ripple on each tap) | Bot: سیستم‌عامل را انتخاب کنید: Buttons: Ubuntu, Debian. User: Ubuntu. Bot: نسخه Ubuntu را انتخاب کنید: Buttons: Ubuntu 24.04, بازگشت, لغو |
| 6 | 0:18–0:24 | Order summary card slides in, rows stagger in one by one | Card title: خلاصه سفارش. Rows: پلن: CX23 / سیستم‌عامل: Ubuntu 24.04 / کارمزد اولین خرید: صفر / قیمت: طبق نرخ روز. Line: با تأیید، شرایط صورتحساب را پذیرفته‌اید. Buttons: تأیید سفارش, لغو |
| 7 | 0:24–0:27 | Tap on "تأیید سفارش". A check mark draws in. Cut before any payment screen | Bot: سفارش شما ثبت شد. مرحله بعد: پرداخت |
| 8 | 0:27–0:30 | Card closes. MN logo returns with the handle | Headline: سرور Hetzner خود را در چند قدم سفارش دهید. Handle: @shop_manovabot. Support: پشتیبانی: @manova_support |

Scene 6 shows no price figure on purpose. Any real figure would depend on the live rate and needs a dated, approved value.

Scene 5 offers Ubuntu and Debian in the OS step. The version list comes from the OS data in the Remotion project, which holds Ubuntu 24.04 and Debian 12 and 13 (live image check, 2026-11-05). The 30 s cut follows the Ubuntu 24.04 path, so Debian versions are not shown on screen. Ubuntu 26.04 and 22.04 are not in the video's OS data.

Audio: none. The video is silent in this build (owner decision, 2026-11-05). No music track and no tap sounds are included.

## 6. Approvals the owner must give

1. **Slogan and fine print.** Approve the slogan and the short fine print. The rule behind it is fixed in the design (section 5.2): the first purchase is fee-free up to 1,000,000 Toman, and a wallet top-up before that purchase removes the benefit. The video must not present the offer as "no fee" in general.
2. **Duration and format.** Approve 30 s, 1080 × 1920, 30 fps, MP4.
3. **Palette and font.** Approve the palette in section 2 and Vazirmatn. The hex values come from a PNG, so confirm them against the original design file if one exists.
4. **Plan shown.** Approve CX23 with no specs and no prices. If specs or prices are wanted, they need a dated, approved source.
5. **OS shown.** Approve Ubuntu 24.04 for the video. Approve Debian only after a live check enables a version.
6. **Simplified flow.** The real order has an access step (SSH key or root password, design section 12.1) and a fuller policy summary (section 9.1). Approve leaving these out of the video, or ask for one extra shot.
7. **Persian copy.** Approve every line in section 5. The design already requires the owner to review Persian text before launch (section 26.2 and section 24.1).
8. **Names and handles.** Confirm the Persian store name, and confirm the handles @shop_manovabot and @manova_support.
9. **Hetzner promotion.** Round 6 requires a provider-terms check before launch (`internal/owner-decisions-round-6.md`, item 1). Confirm that the check is done before any promo that names Hetzner and targets customers in Iran is published.
10. **Music and voice.** Approve a licensed calm track, and confirm no voiceover.

## 7. Open items

- Remotion licence cost depends on your headcount (UNVERIFIED for you).
- Persian shaping and RTL rendering are UNVERIFIED for both tools. They need a short test render before the full build.
- Vazirmatn Latin glyph quality is UNVERIFIED for this project.
- Debian and Ubuntu 26.04 availability is pending a live check (design section 9A.2).
  Resolved 2026-11-05: the live check shows Ubuntu 22.04, 24.04 and 26.04, and Debian 12 and 13, all available and not deprecated. Debian 11 is not in the list.

## 8. Changelog

- **2026-11-05, fine print (owner change 1).** Scene 2 fine print replaced with exactly "اولین خرید بدون کارمزد". The amount limit and cap are removed. Scene 6 row "کارمزد اولین خرید" now reads "صفر" with no cap.
- **2026-11-05, OS scene (owner change 2).** Scene 5 OS step offers Ubuntu and Debian. The version list is data-driven: Ubuntu 24.04, Debian 12, Debian 13. Ubuntu 26.04 and 22.04 are left out. Timing and the Ubuntu 24.04 walkthrough are unchanged.
- **2026-11-05, scene 2 fine print removed (owner change 3).** The fine-print line is removed, so scene 2 shows only the headline "اولین خرید بدون کارمزد". The headline and fine print had the same text.
- **2026-11-05, audio removed (owner change 4).** The video has no music, no voiceover, and no tap sounds. The default music and click-sound lines in section 5 are replaced by "Audio: none".
- **2026-11-05, render fix.** In the outro, the support handle is set as its own left-to-right run, so "@manova_support" reads correctly next to the Persian label. The copy is unchanged.
- **2026-11-05, render.** First build rendered with Remotion. Output: `media/promo/manova-promo-30s.mp4`. Frames: `media/promo/manova-promo-frame-<scene>.png`.
