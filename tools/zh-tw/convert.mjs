#!/usr/bin/env node
// 簡體 → 繁體（台灣用語）轉換器。
//
// 原始碼在 git 裡保持簡體（與上游、協作者一致）：
// 前端由 gen-locale.mjs 依 zh 產生 zh-TW 語言包（提交進 git），
// 後端由 convert-go.mjs 在 Docker 建置時呼叫這裡的函式轉換字串字面值。
//
// 也可當 CLI 對檔案就地轉換（例如檢查或一次性處理）：
//   node tools/zh-tw/convert.mjs [--dry] [--vocab-only] <glob> [glob...]
//
// 以 OpenCC s2twp 為基底，另加兩層修正：
//   CORRECTIONS — 修 OpenCC 的誤判（账号→賬號 應為 帳號、权限→許可權 應為 權限、
//                 回调→回撥 應為 回呼……）
//   TW_VOCAB    — 兩岸同字形但用語不同的詞（郵箱→電子郵件、配置→設定……）
// 只有中文字元會被改動，ASCII 識別字與程式結構不受影響。
//
// 冪等性：OpenCC 的 s2twp 對「已經是繁體」的文字再跑一次會改壞
// （控制代碼→控制程式碼、儀表板→儀錶板）。所以 toTraditional() 會先偵測，
// 純繁體的輸入原樣回傳；convertSource() 更是逐行判斷，讓同一個檔案裡
// 協作者寫的簡體與你自己寫的繁體可以共存。

import { readFileSync, writeFileSync, globSync } from 'node:fs'
import { pathToFileURL } from 'node:url'
import * as OpenCC from 'opencc-js'

const s2twp = OpenCC.Converter({ from: 'cn', to: 'twp' })
// 純字元層級的 cn→tw，不帶詞彙表。只用來偵測「是否含簡體字」。
const s2t = OpenCC.Converter({ from: 'cn', to: 'tw' })

// OpenCC s2twp 的過度轉換 / 用語偏差修正。順序有意義：先長詞後短詞。
const CORRECTIONS = [
  // 「回调」的调→撥 誤判。台灣的 Callback 是「回呼」，「回撥」是打電話回撥。
  [/回撥/g, '回呼'],
  [/對映/g, '對應'],
  [/例項/g, '實例'],
  [/引數/g, '參數'],
  [/全域性/g, '全域'],
  [/型別/g, '類型'],
  [/許可權/g, '權限'],
  [/繫結/g, '綁定'],
  [/指令碼/g, '腳本'],
  [/映象/g, '映像'],
  [/賬/g, '帳'],
  // s2twp 把 二进制 轉成「二進位制」，那不是一個詞
  [/二進位制/g, '二進位'],
  // 台灣現代慣用「台」（平台／控制台／後台），「臺」是較正式的公文用字
  [/臺/g, '台'],
  // OpenCC 把 upstream 的「上游」誤轉成「上遊」
  [/上遊/g, '上游'],
  // OpenCC 詞典誤判成地名／專有名詞而漏轉的簡體字
  [/新余額/g, '新餘額'], // 「新余」被當成江西新余市
  [/天后到期/g, '天後到期'], // 「天后」被當成媽祖
  // 「文件」在法律文件語境不該轉成「檔案」
  [/協議檔案/g, '協議文件'],
  [/條款檔案/g, '條款文件'],
  [/法律檔案/g, '法律文件'],
  [/政策檔案/g, '政策文件'],
  // 法律文件裡的「本文件／該文件」是 document，不是電腦檔案
  [/本檔案/g, '本文件'],
  [/該檔案/g, '該文件'],
  [/檔案、手續/g, '文件、手續'],
  // 注册表 在本專案是 tool registry；「登錄檔」在台灣專指 Windows Registry
  [/登錄檔/g, '註冊表'],
  // 进程（process）被 s2twp 轉成「程序」，台灣讀來是 procedure；這裡指服務的處理程序
  [/程序狀態/g, '處理程序狀態'],
  // 注销（帳號註銷）被 s2twp 轉成「登出」，台灣的 logout 才是登出
  [/帳戶登出/g, '帳號註銷'],
  [/帳號登出/g, '帳號註銷']
]

// 中國慣用語 → 台灣慣用語。這些字在兩岸都是同一個繁體字形，OpenCC 不會處理，
// 但對台灣使用者而言讀起來是「中國口音」，所以另外替換。
// 只放語意明確、不需看上下文就能安全替換的詞。
// 順序有意義：長詞在前，避免短詞先替換後長詞規則失效。
const TW_VOCAB = [
  [/郵箱/g, '電子郵件'],
  [/二維碼/g, 'QR Code'],
  [/移動端/g, '行動版'],
  [/桌面端/g, '桌面版'],
  [/質量/g, '品質'],
  [/請求頭/g, '請求標頭'],
  [/響應頭/g, '回應標頭'],
  [/負載均衡/g, '負載平衡'],
  [/下拉框/g, '下拉選單'],
  [/彈窗/g, '彈出視窗'],
  // 專案原本 渠道／通道 混用同一概念，統一成台灣慣用的「通道」
  [/渠道/g, '通道'],
  [/運維/g, '維運'],
  [/充值/g, '儲值'],
  [/排障/g, '故障排除'],
  [/獲取/g, '取得'],
  [/內容稽核/g, '內容審核'],
  // dashboard 台灣是「儀表板」；儀錶板／儀錶盤 是 s2twp 對繁體再跑一次的產物，一併正規化
  [/儀表盤|儀錶板|儀錶盤/g, '儀表板'],
  [/服務端/g, '伺服器端'],
  // client 統一用「用戶端」（台灣／微軟慣用），避免 客戶端／用戶端 在同一介面混用
  [/客戶端/g, '用戶端'],
  // 「用戶端」是 client 的正確台灣譯法，不可換成「使用者端」
  [/用戶(?!端)/g, '使用者'],
  // 以下是簡繁字形相同的中國用語原形，給「作者寫繁體、但混了中國用語」的行使用
  // （這類行不會過 s2twp，所以要直接認得原形）
  [/配置文件/g, '設定檔'],
  [/刷新令牌/g, '更新權杖'],
  [/刷新/g, '重新整理'],
  // 地址：URL 用網址、IP/主機用位址；「電子郵件地址」在台灣是正確說法，保留
  [/上游地址/g, '上游網址'],
  [/端點地址/g, '端點網址'],
  [/服務地址/g, '服務位址'],
  [/內網地址/g, '內網位址'],
  [/主機地址/g, '主機位址'],
  [/預設地址/g, '預設網址'],
  [/頁面地址/g, '頁面網址'],
  [/應用地址/g, '應用網址'],
  [/官方地址/g, '官方網址'],
  [/基礎地址/g, '基礎網址'],
  [/回跳地址/g, '回跳網址'],
  [/站點地址/g, '網站網址'],
  [/前端地址/g, '前端網址'],
  [/下載地址/g, '下載網址'],
  [/通知地址/g, '通知網址'],
  [/回呼地址/g, '回呼網址'],
  [/授權地址/g, '授權網址'],
  [/請求地址/g, '請求網址'],
  [/IP\s?地址/g, 'IP 位址'],
  // 跳轉：分頁用「前往」，頁面重導向用「導向」
  [/跳轉地址/g, '導向網址'],
  [/跳轉到第/g, '前往第'],
  [/跳轉頁/g, '前往頁'],
  [/正在跳轉到/g, '正在導向'],
  [/自動跳轉/g, '自動導向'],
  [/跳轉狀態/g, '導向狀態'],
  [/跳轉/g, '導向'],
  [/報錯資訊/g, '錯誤訊息'],
  [/頻繁報錯/g, '頻繁錯誤'],
  [/實際報錯為準/g, '實際錯誤訊息為準'],
  [/才報錯/g, '才顯示錯誤'],
  [/不報錯/g, '不顯示錯誤'],
  [/報錯/g, '錯誤'],
  [/響應/g, '回應'],
  [/校驗/g, '驗證'],
  [/配置檔案/g, '設定檔'],
  [/配置目錄/g, '設定目錄'],
  [/配置項/g, '設定項目'],
  [/配置/g, '設定'],
  // 備份 restore 台灣叫「還原」；狀態／連線 recover 才是「恢復」
  [/備份恢復/g, '備份還原'],
  [/恢復備份/g, '還原備份'],
  [/定時備份與恢復/g, '定時備份與還原'],
  // 登入身分用「帳號」；銀行／餘額才用「帳戶」
  [/建立帳戶/g, '建立帳號'],
  [/新帳戶/g, '新帳號'],
  [/登入您的帳戶/g, '登入您的帳號'],
  [/帳戶資訊/g, '帳號資訊'],
  [/沒有帳戶/g, '沒有帳號'],
  [/已有帳戶/g, '已有帳號'],
  // 返回：API／函式產生結果用「回傳」，導覽動作保留「返回」
  [/上游返回/g, '上游回傳'],
  [/後端返回/g, '後端回傳'],
  [/伺服器返回/g, '伺服器回傳'],
  [/介面返回/g, '介面回傳'],
  [/返回的/g, '回傳的'],
  [/返回值/g, '回傳值'],
  [/應該返回/g, '應該回傳'],
  [/返回(結果|資料|內容|錯誤|訊息|狀態碼)/g, '回傳$1'],
  [/按回車/g, '按 Enter'],
  [/回車鍵/g, 'Enter 鍵'],
  [/回車/g, 'Enter'],
  // token 台灣譯「權杖」；token refresh 是「更新」，不是頁面的「重新整理」
  [/重新整理令牌/g, '更新權杖'],
  [/令牌/g, '權杖'],
  [/Token 重新整理/g, 'Token 更新'],
  [/token 重新整理/g, 'token 更新'],
  [/快取非同步重新整理/g, '快取非同步更新'],
  [/快取重新整理/g, '快取更新'],
  // OpenCC 的 s2twp 會把 client 轉成「使用者端」，台灣術語是「用戶端」
  [/使用者端/g, '用戶端'],
  // ---- 以下為 holeenlu/sub2api 產生獨立 zh-TW 語言包時，依 audit-locale.mjs 稽核結果補的台灣用語 ----
  [/當前/g, '目前'],
  [/自定義/g, '自訂'],
  [/審計/g, '稽核'],
  [/告警/g, '警示'],
  [/超時/g, '逾時'],
  [/套餐/g, '方案'],
  [/文本/g, '文字'],
  [/概覽/g, '總覽'],
  [/域名/g, '網域'],
  [/封禁/g, '封鎖'],
  [/退出登入/g, '登出'],
  [/構建/g, '建置'],
  [/流式/g, '串流'],
  [/倉庫/g, '儲存庫'],
  [/拖拽/g, '拖曳'],
  [/證書/g, '憑證'],
  [/創建/g, '建立'],
  [/搜索/g, '搜尋'],
  [/全屏/g, '全螢幕'],
  [/性能/g, '效能'],
  [/歸檔/g, '封存'],
  [/導航/g, '導覽'],
  [/添加/g, '新增'],
  [/打開/g, '開啟'],
  [/併發/g, '並行'],
  [/反饋/g, '回饋'],
  [/排查/g, '檢查'],
  [/脫敏/g, '去識別化'],
  [/全量/g, '完整'],
  [/重置/g, '重設'],
  [/提現/g, '提領'],
  [/標籤頁/g, '分頁'],
  [/兜底/g, '備援'],
  [/對接/g, '串接'],
  [/時間戳(?!記)/g, '時間戳記'],
  [/粘貼/g, '貼上'],
  [/粘/g, '黏'],
  [/終端(?!機|點)/g, '終端機'],
  [/運營/g, '營運'],
  [/合同/g, '合約'],
  // authentication 是「驗證」；認證 在台灣偏向 certification。要放在 憑據 之前，
  // 否則 憑據→認證資訊 會再被改成「驗證資訊」。
  [/認證/g, '驗證'],
  [/憑據/g, '認證資訊'],
  // access：存取（存取權杖、存取金鑰、拒絕存取）
  [/訪問/g, '存取'],
  // rate limit：速率限制
  [/限流/g, '速率限制'],
  // session：工作階段
  [/會話/g, '工作階段'],
  // WebAuthn relying party：信賴方
  [/依賴方/g, '信賴方'],
  // help：說明（幫助您→協助您）
  [/幫助(?=您|你|我|他|使用者)/g, '協助'],
  [/幫助/g, '說明'],
  // placeholder：預留位置
  [/佔位符/g, '預留位置'],
  // apply（動詞）→ 套用；application（名詞）保留「應用」
  [/(立即|已|確認|並|再|後|時)應用(?!程式)/g, '$1套用'],
  [/應用(?=到|於|更新|目前|倍率|後|嗎|此|該|這|模板|模型定價)/g, '套用'],
  [/等應用(?!程式)/g, '等應用程式'],
  [/(驗證器|您的|你的)應用(?!程式)/g, '$1應用程式'],
  [/驗證碼應用金鑰/g, '驗證器應用程式金鑰'],
  // via → 透過；pass（通過驗證／狀態「通過」）保留
  [/(?<![已未不])通過(?= |目前|全域|釘釘|橋接|代理|上游|閘道|網關|此|該|本|用戶端|電子郵件)/g, '透過'],
  [/不通過(?=橋接|代理|上游)/g, '不透過'],
  // 中英文之間補空格（含二維碼→QR Code 造成的黏連）
  [/Enter(?=[一-鿿])/g, 'Enter '],
  [/([一-鿿])QR Code/g, '$1 QR Code'],
  [/QR Code(?=[一-鿿])/g, 'QR Code ']
]

// 純字元層級的 tw→cn，用來偵測「只在繁體才有的字」（帳、設、檔……）。
const t2s = OpenCC.Converter({ from: 'tw', to: 'cn' })

// 這些字在繁體裡本來就常用（平台、上游、公里、批准、餘/余、干、只、面、系……），
// OpenCC 的字元表卻會把它們映射到另一個繁體字，不能拿來當「含簡體」的證據。
const AMBIGUOUS = new Set('台游里准余干只面系才谷志松咸秋占')

const isCJK = (ch) => ch >= '一' && ch <= '鿿'

/** 是否含有確定的簡體字 */
export function hasSimplified(text) {
  for (const ch of text) {
    if (isCJK(ch) && !AMBIGUOUS.has(ch) && s2t(ch) !== ch) return true
  }
  return false
}

/** 是否含有只在繁體才有的字——代表作者寫的是繁體 */
export function hasTraditionalOnly(text) {
  for (const ch of text) {
    if (isCJK(ch) && t2s(ch) !== ch) return true
  }
  return false
}

/**
 * 「已經是繁體」的判準是有繁體獨有的字、且沒有簡體字。
 * 注意：不能用「沒有簡體字」當判準——像「配置文件」「刷新令牌」這種
 * 簡繁字形完全相同的文字，仍然是中國用語，必須交給 s2twp 與 TW_VOCAB 處理。
 */
export function isAlreadyTraditional(text) {
  return !hasSimplified(text) && hasTraditionalOnly(text)
}

/** 只套用台灣慣用語替換，不做簡繁轉換。對已是繁體的文字是安全的。 */
export function toTaiwanVocab(text) {
  return TW_VOCAB.reduce((s, [from, to]) => s.replace(from, to), text)
}

// OpenCC 誤判修正 + 台灣用語。兩者對已是繁體的文字都安全，可以套在任何行上。
function polish(text) {
  const corrected = CORRECTIONS.reduce((s, [from, to]) => s.replace(from, to), text)
  return toTaiwanVocab(corrected)
}

// 行尾加上這個標記可讓該行完全不被轉換（例如比對外部系統文字的字串）。
const KEEP_MARK = /zh-tw:keep/
// 用捕獲群組切分，奇數索引是連續中文段落，偶數索引是其間的非中文文字。
const CJK_RUN = /([一-鿿]+)/
const hasCJK = (text) => /[一-鿿]/.test(text)

/**
 * 一行文字簡→繁。規則：
 * - 帶 `zh-tw:keep` 標記、或沒有中文 → 原樣
 * - 只有繁體獨有的字（作者寫的是繁體）→ 不過 s2twp，只套修正與台灣用語
 * - 含簡體字 → 逐段處理：繁體獨有的段落保留，其餘段落過 s2twp，再整行套修正與用語。
 *   這樣「控制代碼 与 邮箱」不會把已正確的「控制代碼」再翻成「控制程式碼」。
 *   同一段連續中文裡簡繁夾雜仍無法區分，這種情況請用 zh-tw:keep。
 * - 字形中性（配置文件、刷新令牌）→ 視為簡體來源，整行轉換並套台灣用語。
 *   已知歧義：簡體「文件」可指 file 也可指 document，這裡一律當 file → 檔案；
 *   法律文件的「本文件／該文件」由 CORRECTIONS 補回。自己寫繁體要保留「文件」時，
 *   同一行放任何一個繁體獨有的字即可（例如「文件說明」的「說」）。
 */
function convertLine(line) {
  if (KEEP_MARK.test(line) || !hasCJK(line)) return line
  const simplified = hasSimplified(line)
  if (!simplified && hasTraditionalOnly(line)) return polish(line)
  if (!simplified) return polish(s2twp(line))
  const converted = line
    .split(CJK_RUN)
    .map((part, i) => {
      if (i % 2 === 0) return part
      const authoredTraditional = !hasSimplified(part) && hasTraditionalOnly(part)
      return authoredTraditional ? part : s2twp(part)
    })
    .join('')
  return polish(converted)
}

/** 整段文字簡→繁，逐行處理（多行的原始字串也適用）。 */
export function toTraditional(text) {
  return text.split('\n').map(convertLine).join('\n')
}

/** 原始碼轉換：與 toTraditional 相同，逐行處理。 */
export const convertSource = toTraditional

function main() {
  const args = process.argv.slice(2)
  const dry = args.includes('--dry')
  const vocabOnly = args.includes('--vocab-only')
  const patterns = args.filter((a) => !a.startsWith('--'))
  if (patterns.length === 0) {
    console.error('usage: node tools/zh-tw/convert.mjs [--dry] [--vocab-only] <glob> [glob...]')
    process.exit(1)
  }

  const files = [...new Set(patterns.flatMap((p) => globSync(p, { nodir: true })))]
  let changed = 0
  for (const file of files) {
    const src = readFileSync(file, 'utf8')
    const out = vocabOnly ? toTaiwanVocab(src) : convertSource(src)
    if (out === src) continue
    changed++
    if (!dry) writeFileSync(file, out, 'utf8')
    console.log(`${dry ? '[dry] ' : ''}${file}`)
  }
  console.log(`\n${changed} / ${files.length} files ${dry ? 'would change' : 'converted'}`)
}

const entry = process.argv[1]
if (entry && import.meta.url === pathToFileURL(entry).href) main()
