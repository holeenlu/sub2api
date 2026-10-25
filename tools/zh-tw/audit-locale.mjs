#!/usr/bin/env node
// 稽核產生的 zh-TW 語言包：列出疑似殘留的中國用語、OpenCC 誤轉與簡體字。
//
//   node tools/zh-tw/audit-locale.mjs            # 摘要（每個詞的出現次數與一個例句）
//   node tools/zh-tw/audit-locale.mjs --all      # 印出全部命中行
//
// 只是提示，不是硬規則：命中不代表一定要改（例如「上下文」在台灣 AI 語境也通用）。
// 決定要改時，把規則加進 convert.mjs 的 TW_VOCAB / CORRECTIONS 後重跑 gen-locale.mjs。

import { readFileSync, globSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { hasSimplified } from './convert.mjs'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const TW_DIR = join(ROOT, 'frontend/src/i18n/locales/zh-TW')

// 兩岸同字形、但台灣讀者會覺得是「中國用語」的詞；以及 OpenCC s2twp 常見的誤轉。
const SUSPECTS = [
  // 中國用語（簡繁同形，OpenCC 不處理）
  '當前', '超時', '反饋', '概覽', '手機號(?!碼)', '視頻', '音頻', '圖像', '文本', '註銷', '登錄', '退出登',
  '添加', '訪問', '認證', '告警', '報警', '批量', '導航', '拖拽', '全屏', '佔位', '正則', '通配', '縮進',
  '註釋', '高亮', '代碼', '源碼', '倉庫', '構建', '發佈', '依賴', '模塊', '插件', '擴展', '集成', '調試',
  '性能', '併發', '限流', '集群', '證書', '哈希', '算法', '會話', '消息', '流式', '異步', '隊列', '調度',
  '卸載', '默認', '自定義', '高級', '全局', '變量', '憑據', '審計', '封禁', '拉黑', '存儲', '歸檔',
  '回收站', '智能', '優化', '支持', '通過', '創建', '粘貼', '保存', '加載', '打開', '點擊', '界面',
  '程序', '項目', '質量', '網關', '視圖', '光標', '字段', '用戶', '服務端', '客戶端', '賬', '郵箱',
  '二維碼', '渠道', '充值', '令牌', '回撥', '設置', '鼠標', '屏幕', '菜單', '窗口', '文件夾', '字節',
  '緩存', '內存', '硬盤', '分辨率', '兼容', '函數', '對象', '數組', '字符', '循環', '線程', '進程',
  '端口', '域名', '鏈接', '搜索', '短信', '信息', '軟件', '硬件', '網絡', '服務器', '數據', '幫助',
  '套餐', '暗色', '亮色', '生成', '稍後', '條記錄', '條資料', '條日誌', '刷新', '校驗', '報錯', '跳轉',
  '返回', '回車', '配置', '獲取', '運維', '移動端', '桌面端', '質量', '請求頭', '響應', '下拉框', '彈窗',
  '恢復', '一周', '周期', '周報', '每周', '本周', '上周', '登出', '退出', '注銷', '主頁', '首頁', '應用',
  '服務商', '運行', '執行緒', '開源', '社區', '交互', '體驗', '在線', '離線', '實時', '即時',
  // OpenCC s2twp 誤轉（已在 CORRECTIONS 處理者應為 0）
  '例項', '引數', '型別', '許可權', '繫結', '指令碼', '儀錶', '對映', '全域性', '映象', '二進位制',
  '上遊', '登錄檔', '控制程式碼', '天后', '新余', '賬', '臺'
]

const files = globSync('**/*.ts', { cwd: TW_DIR }).map((f) => join(TW_DIR, f))
const all = process.argv.includes('--all')
const hits = new Map()
let simplifiedLines = 0

for (const file of files) {
  const rel = relative(ROOT, file)
  readFileSync(file, 'utf8')
    .split('\n')
    .forEach((line, i) => {
      if (/^\s*\/\//.test(line)) return
      if (hasSimplified(line)) {
        simplifiedLines++
        if (all) console.log(`[簡體殘留] ${rel}:${i + 1}: ${line.trim()}`)
      }
      for (const word of SUSPECTS) {
        if (new RegExp(word).test(line)) {
          const entry = hits.get(word) || { count: 0, sample: '' }
          entry.count++
          if (!entry.sample) entry.sample = `${rel}:${i + 1}: ${line.trim().slice(0, 110)}`
          hits.set(word, entry)
          if (all) console.log(`[${word}] ${rel}:${i + 1}: ${line.trim()}`)
        }
      }
    })
}

console.log(`\n簡體殘留行數：${simplifiedLines}`)
console.log('疑似用語（次數 / 例句）：')
for (const [word, { count, sample }] of [...hits.entries()].sort((a, b) => b[1].count - a[1].count)) {
  console.log(`  ${word.padEnd(8)} ${String(count).padStart(4)}  ${sample}`)
}
