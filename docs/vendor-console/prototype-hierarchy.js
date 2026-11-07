/* V2 design workspace. Reuses the existing account/proxy workbenches and fixtures. */
'use strict';
(() => {
  const model = VendorHierarchyModel;
  const words = {
    childAccounts: ['二级账号池','Tier-2 account pools','二級帳號池','第2階層のアカウント'], childProxies: ['二级代理','Tier-2 proxies','二級代理','第2階層のプロキシ'],
    readOnly: ['只读查看','Read only','唯讀檢視','閲覧のみ'], childReadHelp: ['仅查看直属二级供应商的资源、状态与历史，不可编辑、删除或变更供给。','View direct children’s resources, status and history. Editing, deletion and supply changes are unavailable.','僅檢視直屬二級供應商的資源、狀態與歷史，不可編輯、刪除或變更供給。','直属下位のリソース・状態・履歴を閲覧できます。編集・削除・供給変更はできません。'],
    allChildren: ['全部直属二级','All direct tier-2 suppliers','全部直屬二級','直属の第2階層すべて'], resourceSearch: ['搜索名称 / ID / 地址','Search name / ID / address','搜尋名稱 / ID / 位址','名前・ID・アドレスを検索'], notArchived: ['未归档','Not archived','未封存','未アーカイブ'],
    owner: ['所属供应商','Owner supplier','所屬供應商','所有サプライヤー'], resourceType: ['平台 / 协议','Platform / protocol','平台 / 協定','プラットフォーム・プロトコル'], anyType: ['全部平台 / 协议','All platforms / protocols','全部平台 / 協定','すべての種類'],
    quotaLabel: ['配额 / 重置时间','Quota / reset time','配額 / 重設時間','クォータ・リセット日時'], endpoint: ['代理地址','Proxy address','代理位址','プロキシアドレス'], location: ['国家 / 地区','Country / region','國家 / 地區','国・地域'], bindings: ['绑定账号','Bound accounts','綁定帳號','関連アカウント'], createdAt: ['创建时间','Created at','建立時間','作成日時'],
    archivedStop: ['已归档 · 停止供给','Archived · Supply stopped','已封存 · 停止供給','アーカイブ済み・供給停止'],
    archiveTitle: ['删除账号（仅归档）','Delete account (archive only)','刪除帳號（僅封存）','アカウント削除（アーカイブのみ）'],
    archiveNotice: ['此操作仅归档账号并停止供给，不会从数据库或账号池列表中删除。','This archives the account and stops supply. It stays in the database and account pool.','此操作僅封存帳號並停止供給，不會從資料庫或帳號池清單刪除。','供給を停止しアーカイブします。データベースとアカウント一覧には残ります。'],
    archiveDescription: ['账号将显示“已归档”，保留账号资料、授权、分组、代理关联、用量、账单和审计历史。归档后只可查看详情与用量。','The account is marked Archived. Account data, credentials, groups, proxy links, usage, statements and audits are retained. Details and usage remain available.','帳號將顯示「已封存」，保留帳號資料、授權、群組、代理關聯、用量、帳單和稽核歷史。封存後僅能檢視詳情與用量。','アーカイブ済みと表示し、認証、グループ、プロキシ関連、使用量、明細、監査履歴を保持します。詳細と使用量は閲覧できます。'],
    archiveConfirm: ['我确认归档此账号并停止供给','I confirm archiving this account and stopping supply','我確認封存此帳號並停止供給','アーカイブして供給を停止することを確認'], archiveSubmit: ['确认归档','Confirm archive','確認封存','アーカイブを確認'],
    portalDemo: ['演示邮箱：vendor-a@example.invalid（一级）、vendor-b@example.invalid（二级）。不输入真实密码，不连接认证服务。','Demo email: vendor-a@example.invalid (tier 1), vendor-b@example.invalid (tier 2). No real password or authentication service.','演示信箱：vendor-a@example.invalid（一級）、vendor-b@example.invalid（二級）。不輸入真實密碼，不連接認證服務。','デモ用メール：vendor-a@example.invalid（第1階層）、vendor-b@example.invalid（第2階層）。実際のパスワードや認証サービスは使用しません。'],
    portalCreators: ['一级由超级管理员开通；二级由所属一级开通。无公开注册。','Super administrators create tier 1; parents create tier 2. No public registration.','一級由超級管理員開通；二級由所屬一級開通。無公開註冊。','第1階層はスーパー管理者、第2階層はその上位が作成。一般登録はありません。'],
    archivedAccount: ['已归档','Archived','已封存','アーカイブ済み'],
    contact: ['联系人','Contact','聯絡人','連絡先'], settlementEmail: ['结算邮箱','Settlement email','結算信箱','精算メール'], recipient: ['币安 ID（演示）','Binance ID (demo)','幣安 ID（演示）','Binance ID（デモ）'],
    childCap: ['直属二级数量上限','Direct child limit','直屬二級數量上限','直属下位の数の上限'], concurrency: ['默认并发','Default concurrency','預設並行數','既定同時実行数'], priority: ['默认优先级','Default priority','預設優先順序','既定優先順位'], statisticsRate: ['账号统计倍率','Account statistics multiplier','帳號統計倍率','アカウント統計倍率'], direct: ['允许无代理直连','Allow direct connections','允許無代理直連','直接接続を許可'], defaults: ['账号创建默认值','New account defaults','帳號建立預設值','新規アカウントの既定値'],
    accountSettings: ['账号设置','Account settings','帳號設定','アカウント設定'], info: ['账户信息','Account information','帳戶資訊','アカウント情報'], password: ['修改密码','Change password','修改密碼','パスワード変更'], currentPassword: ['当前密码','Current password','目前密碼','現在のパスワード'], newPassword: ['新密码','New password','新密碼','新しいパスワード'], confirmPassword: ['确认新密码','Confirm new password','確認新密碼','新しいパスワードを確認'], passwordError: ['请填写当前密码，新密码至少12位且两次一致','Enter the current password and matching new passwords of at least 12 characters','請填目前密碼，新密碼至少12位且兩次一致','現在のパスワードと、一致する12文字以上の新パスワードを入力'], logout: ['退出登录','Sign out','登出','ログアウト'], registered: ['注册时间','Registered at','註冊時間','登録日時'],
    supplyGroups: ['分组供给','Group supply','群組供給','グループ供給'], byModel: ['按模型','By model','按模型','モデル別'], accountDate: ['账号 × 日期','Account × date','帳號 × 日期','アカウント × 日付'], accountFilter: ['供给账号','Supply account','供給帳號','供給アカウント'], allAccountsFilter: ['全部账号（含归档）','All accounts (including archived)','全部帳號（含封存）','全アカウント（アーカイブ済み含む）'],
    supplierCount: ['供应商数量','Suppliers','供應商數量','サプライヤー数'], overview: ['经营总览','Overview','經營總覽','概要'], organization: ['供应商组织','Supplier organization','供應商組織','サプライヤー組織'],
    children: ['二级供应商','Tier-2 suppliers','二級供應商','第2階層サプライヤー'], tier1: ['一级供应商','Tier-1 supplier','一級供應商','第1階層サプライヤー'], tier2: ['二级供应商','Tier-2 supplier','二級供應商','第2階層サプライヤー'],
    root: ['超级管理员','Super administrator','超級管理員','スーパー管理者'], admin: ['普通管理员','Administrator','一般管理員','管理者'],
    accounts: ['我的账号池','My account pool','我的帳號池','自分のアカウント'], proxies: ['我的专属代理','My proxies','我的專屬代理','自分のプロキシ'],
    allAccounts: ['供应商账号','Supplier accounts','供應商帳號','供給アカウント'], allProxies: ['供应商代理','Supplier proxies','供應商代理','供給プロキシ'],
    usage: ['供给用量','Supply usage','供給用量','供給使用量'], finance: ['结算中心','Settlements','結算中心','精算センター'], audit: ['操作审计','Audit log','操作稽核','監査ログ'],
    work: ['工作区','Workspace','工作區','ワークスペース'], business: ['经营与结算','Business & settlements','經營與結算','事業・精算'], settings: ['偏好设置','Preferences','偏好設定','環境設定'],
    language: ['语言','Language','語言','言語'], theme: ['显示模式','Display mode','顯示模式','表示モード'], light: ['浅色','Light','淺色','ライト'], dark: ['深色','Dark','深色','ダーク'], auto: ['跟随系统','System','跟隨系統','システムに従う'],
    demo: ['V2 交互原型 · 虚构数据','V2 prototype · Fictitious data','V2 互動原型 · 虛構資料','V2 プロトタイプ・架空データ'], review: ['评审身份（仅原型）','Preview identity (prototype only)','評審身分（僅原型）','プレビュー用の権限'],
    own: ['自有供给','Own supply','自有供給','自社供給'], downstream: ['下级供给','Child supply','下級供給','下位の供給'], all: ['全部供给','All supply','全部供給','すべての供給'],
    identity: ['角色与范围','Role & scope','角色與範圍','役割と範囲'], hierarchyHelp: ['独立供应商角色 · 自有资源与下级经营分开管理','Independent supplier roles · Own resources and child operations','獨立供應商角色 · 自有資源與下級經營分開管理','独立した役割・自社リソースと下位管理を分離'],
    platform: ['平台','Platform','平台','プラットフォーム'], parent: ['上级','Parent','上級','上位'], name: ['名称','Name','名稱','名前'], email: ['登录邮箱','Login email','登入信箱','ログインメール'],
    status: ['状态','Status','狀態','状態'], active: ['已启用','Active','已啟用','有効'], disabled: ['已停用','Disabled','已停用','停止'], pending: ['待激活','Pending activation','待啟用','有効化待ち'], inherited: ['上级已停用','Parent disabled','上級已停用','上位が停止中'],
    groups: ['开放分组','Allowed groups','開放群組','許可グループ'], resources: ['资源配额','Resource limits','資源配額','リソース上限'], operation: ['操作','Actions','操作','操作'],
    create1: ['创建一级供应商','Create tier-1 supplier','建立一級供應商','第1階層を作成'], create2: ['创建二级供应商','Create tier-2 supplier','建立二級供應商','第2階層を作成'],
    configure: ['配置','Configure','設定','設定'], detail: ['详情','Details','詳情','詳細'], contract: ['合同价格','Contract prices','合約價格','契約価格'], enable: ['启用','Enable','啟用','有効化'], disable: ['停用','Disable','停用','停止'],
    close: ['关闭','Close','關閉','閉じる'], cancel: ['取消','Cancel','取消','キャンセル'], save: ['保存配置','Save configuration','儲存設定','設定を保存'], confirm: ['确认','Confirm','確認','確認'],
    search: ['搜索名称 / 邮箱','Search name / email','搜尋名稱 / 信箱','名前・メールで検索'], anyStatus: ['全部状态','All statuses','全部狀態','すべての状態'],
    parentRule: ['角色和上级由创建者确定，不能扩大上级授权范围。','Role and parent are assigned by the creator. Permissions cannot exceed the parent.','角色和上級由建立者確定，不得擴大上級授權。','役割と上位は作成者から決定。上位の権限を超える設定は禁止。'],
    identityStep: ['身份与配额','Identity & limits','身分與配額','ID と上限'], priceStep: ['价格与复核','Pricing & review','價格與複核','価格と確認'], next: ['下一步','Next','下一步','次へ'], back: ['上一步','Back','上一步','戻る'],
    inviteHelp: ['创建后待激活，通过一次性邀请设置密码。原型不发送邮件。','Created as pending activation. Set a password with a single-use invitation. No email is sent by this prototype.','建立後待啟用，透過單次邀請設定密碼。原型不寄信。','作成後は有効化待ち。一度限りの招待でパスワード設定。デモでは送信しません。'],
    createInvite: ['创建并生成邀请','Create & prepare invitation','建立並產生邀請','作成して招待を準備'], activation: ['模拟激活','Simulate activation','模擬啟用','有効化をシミュレート'],
    accountLimit: ['账号数量上限','Account limit','帳號數量上限','アカウント上限'], proxyLimit: ['代理数量上限','Proxy limit','代理數量上限','プロキシ上限'],
    price: ['合同单价（CNY / USD）','Unit price (CNY / USD)','合約單價（CNY / USD）','単価 (CNY / USD)'], multiplier: ['结算倍率','Settlement multiplier','結算倍率','精算倍率'],
    fx: ['换算价（CNY / USDT）','Exchange value (CNY / USDT)','換算價（CNY / USDT）','換算値 (CNY / USDT)'], effective: ['生效时间','Effective from','生效時間','適用開始'],
    ratesHelp: ['分组留空使用默认单价；与账号统计倍率独立。改价仅对未来生效。','Blank group rates use the default price. Independent of account statistics. Price changes apply prospectively.','群組留空使用預設單價；與帳號統計倍率獨立。調價只對未來生效。','空欄は既定単価。統計倍率とは独立。変更は将来のみに適用。'],
    preview: ['重新计算','Recalculate','重新計算','再計算'],
    sample: ['试算：虚构基数 1,000 USD','Estimate: fictitious basis 1,000 USD','試算：虛構基數 1,000 USD','試算：架空の基準 1,000 USD'], upstream: ['上游应收','Upstream receivable','上游應收','上位からの売掛'], payable: ['下游应付','Downstream payable','下游應付','下位への買掛'],
    platformPayable: ['平台应付','Platform payable','平台應付','プラットフォーム支払'], margin: ['下级供给毛差','Child supply margin','下級供給毛差','下位供給の粗差額'],
    negative: ['预计负毛差，请核对下游报价。仍可保存。','Estimated negative margin. Check the child price; saving is allowed.','預計負毛差，請核對下游報價，仍可儲存。','差額がマイナス。下位単価を確認してください。保存は可能です。'],
    twoLegs: ['同一份供给事实，两份独立合同；两层账单分别确认、付款。','One supply fact, two independent contracts. Each settlement leg is confirmed and paid separately.','同一份供給事實、兩份獨立合約；兩層帳單各自確認及付款。','一つの供給事実、二つの独立契約。各精算を個別に確認・支払。'],
    requests: ['请求数','Requests','請求數','リクエスト'], cost: ['结算基数 USD（演示）','Settlement basis USD (demo)','結算基數 USD（演示）','精算基準 USD（デモ）'], accountsCount: ['供给账号','Supply accounts','供給帳號','供給アカウント'],
    estimate: ['预计结算 · 非已确认账款','Estimated · Not confirmed liabilities','預計結算 · 非已確認帳款','見積・確定債務ではありません'],
    start: ['开始日期','Start date','開始日期','開始日'], end: ['结束日期','End date','結束日期','終了日'], dimension: ['聚合维度','Group by','彙總維度','集計単位'], bySupplier: ['按供应商','By supplier','按供應商','供給者別'], byAccount: ['按账号','By account','按帳號','アカウント別'], byDate: ['按日期','By date','按日期','日付別'], byGroup: ['按服务分组','By served group','按服務群組','サービスグループ別'],
    export: ['导出筛选明细','Export filtered details','匯出篩選明細','絞り込み明細を出力'], apply: ['应用筛选','Apply filters','套用篩選','適用'], empty: ['暂无符合条件的数据','No matching data','暫無符合條件的資料','該当データなし'],
    historyHelp: ['包含已归档账号历史；按请求实际服务分组计一次。金额为虚构基数。','Includes archived-account history. Counted once by the actual served group. Monetary bases are fictitious.','包含已封存帳號歷史；按實際服務群組計一次。金額為虛構基數。','アーカイブ済み履歴を含む。実際のグループで一度計上。金額は架空。'],
    incoming: ['上游账单','Upstream statements','上游帳單','上位明細'], outgoing: ['下游账单','Downstream statements','下游帳單','下位明細'], contracts: ['合同与报价','Contracts & pricing','合約與報價','契約と価格'],
    counterparty: ['交易对方','Counterparty','交易對方','取引先'], period: ['账期','Period','帳期','対象期間'], amount: ['应结 USDT','Amount USDT','應結 USDT','精算額 USDT'], paid: ['已付 USDT','Paid USDT','已付 USDT','支払済 USDT'],
    draft: ['草稿','Draft','草稿','下書き'], reviewBill: ['待对方确认','Awaiting confirmation','待對方確認','相手の確認待ち'], confirmed: ['已确认','Confirmed','已確認','確認済'], disputed: ['有异议','Disputed','有異議','異議あり'], partial: ['部分付款','Partially paid','部分付款','一部支払'], paidState: ['已结清','Paid','已結清','支払済'], void: ['已作废','Void','已作廢','取消済'],
    generate: ['生成结算草稿','Create statement draft','產生結算草稿','精算下書きを作成'], send: ['提交对账','Send for review','提交對帳','照合を依頼'], confirmBill: ['确认账单','Confirm statement','確認帳單','明細を確認'], dispute: ['提出异议','Dispute','提出異議','異議を申立て'], payment: ['登记付款','Record payment','登記付款','支払を記録'], voidBill: ['作废账单','Void statement','作廢帳單','明細を取消'],
    reason: ['原因 / 说明','Reason / note','原因 / 說明','理由・備考'], reference: ['付款凭证号','Payment reference','付款憑證號','支払参照番号'],
    frozen: ['账单已冻结用量、合同版本和金额；后续改价不影响本单。','Usage, contract version and amounts are frozen. Later prices do not change this statement.','帳單已凍結用量、合約版本與金額；調價不影響本單。','使用量・契約版・金額を固定。将来の価格変更は影響しません。'],
    preserved: ['停用保留授权、账号和历史；上级恢复不解除下级自身停用。','Disabling retains authorizations and history. Re-enabling a parent does not re-enable a disabled child.','停用保留授權及歷史；上級恢復不解除下級自身停用。','停止しても認証と履歴は保持。上位再開で下位の停止は解除しません。'],
    invitation: ['邀请已准备（模拟）','Invitation prepared (demo)','邀請已備妥（模擬）','招待を準備（デモ）'], saved: ['配置已保存','Configuration saved','設定已儲存','保存しました'],
    forbidden: ['无权限执行此操作','This operation is not permitted','無權執行此操作','この操作は許可されていません'], selectGroups: ['至少选择一个平台分组','Select at least one platform group','至少選擇一個平台群組','グループを選択してください'],
    invalidLimits: ['请填写有效的资源上限','Enter valid resource limits','請填寫有效資源上限','有効な上限を入力してください'], duplicateEmail: ['此邮箱已关联供应商','Email already belongs to a supplier','此信箱已關聯供應商','メールが登録済みです'],
    parentSubset: ['不能超出上级开放的平台与分组','Permissions exceed the parent scope','不得超出上級開放範圍','上位の許可範囲を超えています'], parentLimits: ['配额不能超过上级总上限','Limits exceed the parent total','配額不得超過上級總上限','上位の総上限を超えています'], childLimit: ['已达到二级供应商数量上限','Child supplier limit reached','已達二級供應商數量上限','下位の数の上限です'],
    invalidDecimal: ['价格或倍率必须为非负数，最多六位小数','Price and multiplier must be nonnegative with up to six decimal places','價格或倍率須非負，最多六位小數','価格と倍率は非負、小数6桁まで'], positiveExchange: ['换算价必须大于零','Exchange value must be positive','換算價必須大於零','換算値は正数が必要'], missingContract: ['缺少生效合同，不能计算金额','Missing effective contract; amount unavailable','缺少生效合約，無法計算金額','有効な契約なし、計算不可'],
    invalidIdentity: ['请填写名称与 @example.invalid 演示邮箱','Enter a name and an @example.invalid demo email','請填寫名稱及 @example.invalid 演示信箱','名前と @example.invalid デモメールを入力'], invalidDates: ['请选择有效的闭合账期','Choose a valid closed period','請選有效的已結束帳期','終了済みの期間を選択'],
    overlap: ['本结算边已有占用用量；请选择未结账期或先作废旧单','Usage already allocated on this leg. Choose another period or void the unpaid statement.','本結算邊用量已占用，請改帳期或先作廢舊單','この精算で計上済み。別期間を選ぶか未払明細を取消'],
    invalidPayment: ['金额应大于零且不超过未付金额，并填写凭证号','Enter a positive amount within the unpaid balance and a reference','金額須大於零且不超過未付餘額，並填憑證號','未払残高以内の正額と参照番号を入力'],
    futureOnly: ['新价格必须晚于演示当前时点生效','New prices must take effect after the demo current time','新價格須晚於演示目前時間生效','新価格はデモの現在より後に設定'],
    noPermission: ['该角色没有供应商管理权限','This role has no supplier management access','此角色無供應商管理權限','この役割には供給者管理権限がありません'],
    actor: ['操作者','Actor','操作者','操作者'], time: ['时间','Time','時間','時刻'], result: ['结果','Result','結果','結果'], ownOnly: ['仅自己的资源','Own resources only','僅自己的資源','自分のリソースのみ'],
    treeScope: ['自有与直属二级','Own and direct children','自有與直屬二級','自社と直属下位'], globalScope: ['全局供应商治理','Global supplier governance','全域供應商治理','全体の供給者管理'],
    languageScope: ['分组、账号名称为演示业务数据；旧资源工作台的长说明保留中文。','Group/account names are demo data. Long guidance in legacy resource screens remains in Chinese.','群組與帳號名稱為演示資料；舊資源工作台長說明保留中文。','名称はデモデータです。旧リソース画面の長い説明は中国語です。'],
    readContract: ['仅可查看与自己相关的合同','Only contracts involving your identity are visible','僅能查看自身相關合約','自分が当事者の契約のみ表示'], rows: ['条','rows','筆','件'], prev: ['上一页','Previous','上一頁','前へ'], nextPage: ['下一页','Next','下一頁','次へ'],
  };
  Object.assign(words, {
    unarchivedAccounts: ['未归档账号','Unarchived accounts','未封存帳號','未アーカイブのアカウント'],
    operationsView: ['运营明细','Operational details','營運明細','運用明細'], modelCostView: ['模型成本','Model costs','模型成本','モデル費用'],
    reportCost: ['统计消耗 USD','Usage cost USD','統計消耗 USD','統計費用 USD'], opusCost: ['Opus 消耗 USD','Opus cost USD','Opus 消耗 USD','Opus 費用 USD'], fableCost: ['Fable 消耗 USD','Fable cost USD','Fable 消耗 USD','Fable 費用 USD'], otherCost: ['其他 / 未分类 USD','Other / unclassified USD','其他 / 未分類 USD','その他・未分類 USD'],
    tokensIO: ['Token 输入 / 输出','Input / output tokens','Token 輸入 / 輸出','入力・出力トークン'], tokensWithCache: ['Tokens（含缓存）','Tokens (including cache)','Tokens（含快取）','トークン（キャッシュ含む）'],
    cacheIO: ['缓存写入 / 读取','Cache write / read','快取寫入 / 讀取','キャッシュ書込・読取'], cacheRate: ['缓存命中率','Cache hit rate','快取命中率','キャッシュヒット率'],
    inputLabel: ['入','In','入','入力'], outputLabel: ['出','Out','出','出力'], writeLabel: ['写','Write','寫','書込'], readLabel: ['读','Read','讀','読取'],
    source: ['创建来源','Created by','建立來源','作成元'], unknownSource: ['未记录创建来源','Creation source unavailable','未記錄建立來源','作成元の記録なし'],
    reportSummary: ['筛选范围合计','Filtered total','篩選範圍合計','絞り込み合計'], sortBy: ['排序依据','Sort by','排序依據','並べ替え'], sortDirection: ['排序方向','Direction','排序方向','順序'], desc: ['从高到低','Descending','由高到低','降順'], asc: ['从低到高','Ascending','由低到高','昇順'],
    reportSearch: ['搜索账号 / ID','Search account / ID','搜尋帳號 / ID','アカウント・IDを検索'], currentState: ['当前账号状态','Current account state','目前帳號狀態','現在のアカウント状態'], currentHealth: ['当前健康状态','Current health','目前健康狀態','現在の稼働状態'],
    reportHelp: ['消耗为账号统计成本，含缓存费用；Opus + Fable + 其他 / 未分类 = 总消耗，不等于配额或应付。缓存命中率 = 读取 ÷（输入 + 写入 + 读取），按汇总量计算。状态为当前状态，归属按请求时快照。','Costs include cache charges. Opus + Fable + other/unclassified = total usage cost, not quota or payable. Cache hit rate = reads / (input + writes + reads), calculated from totals. Status is current; ownership is recorded at request time.','消耗為帳號統計成本，含快取費用；Opus + Fable + 其他 / 未分類 = 總消耗，不等於配額或應付。命中率按讀取 ÷（輸入 + 寫入 + 讀取）彙總。狀態為目前狀態，歸屬依請求快照。','費用はキャッシュ料金を含み、Opus + Fable + その他・未分類 = 合計。クォータや支払額ではありません。ヒット率は読取 ÷（入力 + 書込 + 読取）の合算。状態は現在、所有者はリクエスト時点。'],
    pool: ['账号池','Account pool','帳號池','アカウントプール'], poolHelp: ['管理自己的 Claude / OpenAI 账号与供给状态','Manage your Claude / OpenAI supply accounts','管理自己的 Claude / OpenAI 帳號與供給狀態','自分の Claude / OpenAI 供給アカウントを管理'],
    poolOverview: ['账号概览（我的账号）','Account overview (my accounts)','帳號概覽（我的帳號）','アカウント概要（自分のアカウント）'], reload: ['↻ 刷新','Refresh','重新整理','更新'], more: ['更多操作 ⌄','More actions','更多操作','その他の操作'], add: ['＋ 添加账号','Add account','新增帳號','アカウントを追加'], cumulative: ['累计 ⓘ','All time','累計','累計'],
    ownAll: ['全量自有账号','All owned accounts','全部自有帳號','すべての自社アカウント'], unknownReset: ['重置时间未知','Reset time unknown','重設時間未知','リセット時刻不明'],
    allPlatforms: ['全部平台','All platforms','全部平台','全プラットフォーム'], allGroups: ['全部分组','All groups','全部群組','全グループ'], allMethods: ['全部授权方式','All authorization methods','全部授權方式','すべての認証方式'], createdSort: ['创建时间 ↓','Created ↓','建立時間 ↓','作成日時 ↓'], todaySort: ['今日消耗 ↓','Today cost ↓','今日消耗 ↓','本日費用 ↓'], allCostSort: ['累计消耗 ↓','Lifetime cost ↓','累計消耗 ↓','累計費用 ↓'],
    columns: ['☷ 列显示','Columns','欄位顯示','表示列'], moreFilters: ['更多筛选','More filters','更多篩選','追加フィルター'], fault: ['故障分布','Failure distribution','故障分佈','障害分布'], cachedTpm: ['含缓存','Includes cache','含快取','キャッシュ含む'], inputOutput: ['输入 + 输出','Input + output','輸入 + 輸出','入力 + 出力'], concurrencyHint: ['当前 / 配置上限','Current / configured maximum','目前 / 設定上限','現在 / 設定上限'],
    statsExplain: ['统计口径说明','Metric definitions','統計口徑說明','指標の定義'], closeModal: ['关闭弹窗','Close dialog','關閉彈窗','ダイアログを閉じる'], noRows: ['此范围没有记录','No matching records','此範圍沒有紀錄','該当記録なし'],
    fixtureNote: ['业务数据仅在内存；语言与主题保存在本机。刷新重置业务数据，未连接真实后台。','Business data stays in memory; language and theme stay on this device. Reload resets demo data. No live backend is connected.','業務資料僅在記憶體；語言與主題保存在本機。重新整理重設演示資料，未連接真實後端。','業務データはメモリ内、言語とテーマは端末に保存。再読込でデモを初期化。実際のバックエンドには未接続。'],
  });
  const localeCodes = ['zh-CN', 'en', 'zh-TW', 'ja'];
  const getPreference = (key, fallback) => { try { return localStorage.getItem(key) || fallback; } catch { return fallback; } };
  const setPreference = (key, value) => { try { localStorage.setItem(key, value); } catch { /* file:// can deny storage */ } };
  const h = window.vendorHierarchy = {
    actor: { role: 'vendor_level1', id: 1 }, page: 'overview', search: '', status: 'all', currentPage: 1,
    locale: getPreference('vendor-prototype-locale', 'zh-CN'), theme: getPreference('vendor-prototype-theme', 'auto'),
    scope: 'all', vendorFilter:'all',accountFilter:'all', from: '2026-10-09', to: '2026-10-14', dimension: 'account', usageMode:'operations', usageSort:'cost', usageOrder:'desc', usageState:'all', usageSearch:'', usagePlatform:'all', usageGroup:'all', financeTab: 'incoming', fx: '6.7',
    contracts: [], bills: [], events: [], edit: null, step: 1,
    resourceVendor:'all', resourceSearch:'', resourceState:'all', resourceType:'all',
  };
  if (!localeCodes.includes(h.locale)) h.locale = 'zh-CN';
  if (!['light', 'dark', 'auto'].includes(h.theme)) h.theme = 'auto';
  const t = key => words[key]?.[localeCodes.indexOf(h.locale)] || key;
  const iconPaths = { sun:'M12 3v2m6.4.6L17 7M21 12h-2m-.6 6.4L17 17M12 19v2m-5-4-1.4 1.4M5 12H3m4-5L5.6 5.6M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0', moon:'M21 15a9 9 0 0 1-12-12 9 9 0 1 0 12 12', auto:'M3 4h18v13H3zM8 21h8m-4-4v4', globe:'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M3 12h18M12 3a17 17 0 0 1 0 18 17 17 0 0 1 0-18', grid:'M3 3h7v7H3zm11 0h7v7h-7zM3 14h7v7H3zm11 0h7v7h-7z', users:'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2m20 0v-2a4 4 0 0 0-3-3.9M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0m4-3.9a4 4 0 0 1 0 7.8', chart:'M4 20V10m8 10V4m8 16v-7', document:'M6 3h9l4 4v14H6zM14 3v5h5M9 12h7m-7 4h7', clock:'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M12 7v5l4 2', check:'M5 12l4 4L19 6', plus:'M12 5v14M5 12h14', down:'m6 9 6 6 6-6' };
  const icon = name => '<svg class="h-icon" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="'+iconPaths[name]+'"/></svg>';
  const button = (action, label, id = '', cls = '') => '<button class="'+cls+'" data-h-action="'+action+'" data-id="'+esc(id)+'">'+t(label)+'</button>';
  const stateName = v => v.status === 'pending' ? 'pending' : v.status === 'disabled' ? 'disabled' : !model.effective(v, vendors) ? 'inherited' : 'active';
  const statusTag = v => '<span class="badge '+(stateName(v)==='active'?'success':stateName(v)==='pending'?'info':'warning')+'">'+t(stateName(v))+'</span>';
  const roleName = v => t(v.tier === 1 ? 'tier1' : 'tier2');
  const actorName = () => h.actor.role === 'super_admin' ? t('root') : h.actor.role === 'admin' ? t('admin') : ownerPolicy().name;
  const isRoot = () => h.actor.role === 'super_admin';
  const isL1 = () => h.actor.role === 'vendor_level1';
  const isSupplier = () => ['vendor_level1', 'vendor_level2'].includes(h.actor.role);
  const visibleVendors = () => isRoot() ? vendors : vendors.filter(v=>v.id===h.actor.id || (isL1() && v.parentId===h.actor.id));
  const ownRows = () => allLedger().filter(r => visibleVendors().some(v=>v.id===r.leafId));
  const matchesVendor = r => h.vendorFilter==='all'||r.leafId===Number(h.vendorFilter)||r.rootId===Number(h.vendorFilter);
  const rows = () => ownRows().filter(r=>matchesVendor(r)&&(h.accountFilter==='all'||r.account===Number(h.accountFilter))&&r.date>=h.from&&r.date<=h.to&&(h.scope==='all'||(h.scope==='own'?r.leafId===h.actor.id:r.leafId!==h.actor.id))
    &&(h.usagePlatform==='all'||r.platform===h.usagePlatform)&&(h.usageGroup==='all'||r.group===h.usageGroup)
    &&(h.usageState==='all'||(h.usageState==='archived'?findAccount(r.account)?.archived:!findAccount(r.account)?.archived))
    &&(!h.usageSearch||(r.accountName+' #'+r.account).toLowerCase().includes(h.usageSearch.toLowerCase())));
  const cents = value => new Intl.NumberFormat(h.locale,{minimumFractionDigits:2,maximumFractionDigits:2}).format(value/100);
  const table = (headers, items, label) => adminTable(headers.map(t),items,t(label)).replace('class="admin-table"', 'class="admin-table h-table"').replace('此范围没有记录',t('empty'));
  const note = (key,cls='') => '<div class="notice '+cls+'">'+t(key)+'</div>';
  function log(action, target, result) { h.events.unshift({ at:DEMO_NOW, actor:actorName(), actorId:h.actor.id, target, action, result }); }
  function initializeHierarchy() {
    vendors.forEach((v,i)=>{v.tier=[1,2,4].includes(i)?2:1;v.parentId=i===1||i===2?1:i===4?4:null;v.rootId=v.parentId||v.id;v.childLimit=20;});
    vendors[1].status='active'; vendors[2].status='disabled';
    allAccounts().forEach(a=>{a.vendorId=a.vendorId||1;if(a.archived){a.paused=true;a.archivedAt??=DEMO_NOW;}}); allProxies().forEach(p=>{p.vendorId=p.vendorId||1;});
    // Explicit fictitious creation metadata; production must use persisted audit provenance.
    allAccounts().forEach(a=>{const owner=accountVendor(a);a.creatorRole=a.id===105?null:a.id===102?'super_admin':'vendor_level'+owner.tier;a.creatorId=a.id===105?null:a.id===102?0:owner.id;});
    allLedger().forEach(r=>{const owner=vendors.find(v=>v.id===r.vendor),a=findAccount(r.account);r.leafId=r.vendor;r.rootId=owner.rootId;r.accountName=a.name;r.platform=a.platform;r.ownerName=owner.name;r.ownerTier=owner.tier;r.creatorRole=a.creatorRole;r.creatorId=a.creatorId;});
    h.contracts=vendors.map(v=>({id:'C-'+v.id+'-1',version:1,payer:v.parentId||0,payee:v.id,leg:v.tier===1?'platform_to_l1':'l1_to_l2',price:v.tier===1?'1.8':'1.6',multiplier:v.tier===1?'1':'0.95',rates:{},effective:'2026-09-01T00:00:00+08:00'}));
  }
  function setActor(value) {
    const [role, id] = value.split(':');
    const nextId = Number(id) || 1;
    const pool = allAccounts(); accounts.forEach(a=>{a.vendorId ||= h.actor.id;});
    const exits = allProxies(); const facts = allLedger();
    h.actor = {role, id:role==='super_admin'||role==='admin'?0:nextId};
    accounts=pool.filter(a=>a.vendorId===(h.actor.id||1));extraAccounts=pool.filter(a=>!accounts.includes(a));
    proxies=exits.filter(p=>p.vendorId===(h.actor.id||1));extraProxies=exits.filter(p=>!proxies.includes(p));
    LEDGER=facts.filter(r=>r.leafId===(h.actor.id||1));EXTRA_LEDGER=facts.filter(r=>!LEDGER.includes(r));
    previewRole=isRoot()?'super_admin':isSupplier()?'vendor':'admin';
    filters=defaultFilters();paging.accounts.page=1;resetProxyWorkspace();h.currentPage=1;h.scope='all';h.vendorFilter='all';h.accountFilter='all';h.search='';h.status='all';h.financeTab=isRoot()?'outgoing':'incoming';
    h.resourceVendor='all';h.resourceSearch='';h.resourceState='all';h.resourceType='all';
    h.usageState='all';h.usageSearch='';h.usagePlatform='all';h.usageGroup='all';
    h.page='overview';page='overview';$('modal').close();setPreviewNavigation(false);render();
  }
  function applyTheme() {
    const dark = h.theme === 'dark' || (h.theme==='auto' && window.matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.dataset.theme=dark?'dark':'light';document.documentElement.style.colorScheme=dark?'dark':'light';
    setPreference('vendor-prototype-theme',h.theme);
  }
  const media=window.matchMedia('(prefers-color-scheme: dark)');media.addEventListener('change',applyTheme);
  function preferenceMarkup() {
    return '<div class="h-preference"><button id="h-language-trigger" data-h-action="language-menu" aria-label="'+t('language')+'" aria-expanded="false" aria-haspopup="menu">'+icon('globe')+'<span>'+({ 'zh-CN':'简中', 'zh-TW':'繁中', en:'EN', ja:'日本語' }[h.locale])+'</span></button><div id="h-language-menu" class="h-menu" role="menu" hidden>'+[['zh-CN','简体中文'],['zh-TW','繁體中文'],['en','English'],['ja','日本語']].map(([code,label])=>'<button role="menuitemradio" aria-checked="'+(h.locale===code)+'" data-h-action="locale" data-id="'+code+'">'+label+(h.locale===code?icon('check'):'')+'</button>').join('')+'</div></div><div class="h-preference"><button id="h-theme-trigger" data-h-action="theme-menu" aria-label="'+t('theme')+'" title="'+t('theme')+'" aria-haspopup="menu" aria-expanded="false">'+icon(h.theme==='light'?'sun':h.theme==='dark'?'moon':'auto')+'</button><div id="h-theme-menu" class="h-menu" role="menu" hidden>'+['light','dark','auto'].map(mode=>'<button role="menuitemradio" aria-checked="'+(h.theme===mode)+'" data-h-action="theme" data-id="'+mode+'">'+icon(mode==='light'?'sun':mode==='dark'?'moon':'auto')+t(mode)+(h.theme===mode?icon('check'):'')+'</button>').join('')+'</div></div>';
  }
  function navigation() {
    const resourceItems=isRoot()?[['admin-accounts','allAccounts','grid'],['admin-proxies','allProxies','globe'],['admin-groups','supplyGroups','grid']]:[['accounts','accounts','grid'],['proxies','proxies','globe']];
    const sections=[['work',[['overview','overview','chart'],...resourceItems]],['business', [...(isRoot()||isL1()?[[isRoot()?'organization':'children',isRoot()?'organization':'children','users']]:[]),...(isL1()?[['child-accounts','childAccounts','grid'],['child-proxies','childProxies','globe']]:[]),['h-usage','usage','chart'],['h-finance','finance','document'],['h-audit','audit','clock']]]];
    const aside=document.querySelector('.sidebar');
    aside.innerHTML=sections.map(([title,items])=>'<div><p class="overline">'+t(title)+'</p><nav>'+items.map(([key,label,glyph])=>'<button data-h-page="'+key+'" '+(h.page===key?'aria-current="page"':'')+'>'+icon(glyph)+t(label)+'</button>').join('')+'</nav></div>').join('')+'<details class="review-nav" open><summary class="overline">'+t('review')+'</summary><label class="preview-role-label"><select id="preview-role" aria-label="'+t('review')+'"><option value="super_admin:0">'+t('root')+'</option>'+vendors.map(v=>'<option value="vendor_level'+v.tier+':'+v.id+'">'+esc(v.name)+' · '+roleName(v)+'</option>').join('')+'<option value="admin:0">'+t('admin')+'</option></select></label><a class="h-design-link" href="HIERARCHY-DESIGN.md" target="_blank">V2 · '+t('identity')+' ↗</a></details><footer><span class="avatar">'+esc(actorName().slice(0,2))+'</span><div><strong>'+esc(actorName())+'</strong><small>'+t(isRoot()?'globalScope':isL1()?'treeScope':'ownOnly')+'</small></div></footer>';
    $('preview-role').value=h.actor.role+':'+h.actor.id;
    document.querySelector('.review-nav').hidden=portalMode;
    // Legacy render reads these anchors; retain hidden compatibility nodes only.
    aside.insertAdjacentHTML('beforeend','<span id="profile-name" hidden></span><span id="profile-scope" hidden></span><nav id="admin-nav" hidden></nav>');
    let prefs=$('h-preferences');if(!prefs){prefs=document.createElement('div');prefs.id='h-preferences';prefs.className='row';document.querySelector('.profile-wrap').before(prefs);}prefs.innerHTML=preferenceMarkup();
    document.querySelector('.demo').textContent=t('demo');document.querySelector('.brand').innerHTML='<span class="logo">S</span>'+t(isRoot()?'organization':'work');
    $('reset').textContent=h.locale==='en'?'Reset demo':h.locale==='ja'?'デモをリセット':h.locale==='zh-TW'?'重設演示':'重置演示';
    document.documentElement.lang=h.locale;renderProfileCenter();
    $('profile-role').textContent=isRoot()?t('root'):isSupplier()?roleName(ownerPolicy()):t('admin');
    document.querySelector('.footer-note').textContent=t('demo')+' · '+t('fixtureNote');
  }
  const metricCard=(label,value,hint='')=>'<div class="panel h-metric"><span>'+t(label)+'</span><strong>'+value+'</strong><small>'+hint+'</small></div>';
  function pageHeading(title,actions='') {return heading(t(title),t('hierarchyHelp'),actions);}
  function estimateValues(selected) {
    const up=model.quote(selected,h.contracts,'platform_to_l1',h.fx).cents;
    const down=model.quote(selected,h.contracts,'l1_to_l2',h.fx).cents;
    const childUp=model.quote(selected.filter(r=>r.leafId!==r.rootId),h.contracts,'platform_to_l1',h.fx).cents;
    return {up,down,margin:childUp-down};
  }
  function dashboard() {
    const facts=ownRows().filter(r=>r.date>=h.from&&r.date<=h.to),scope=visibleVendors(),stats=total(facts),v=ownerPolicy(),values=estimateValues(facts);
    const financial=isRoot()?metricCard('platformPayable',cents(values.up),'USDT'):isL1()?metricCard('upstream',cents(values.up),'USDT')+metricCard('payable',cents(values.down),'USDT')+metricCard('margin',cents(values.margin),'USDT'):metricCard('upstream',cents(values.down),'USDT');
    return pageHeading('overview',isRoot()?button('new-vendor','create1','', 'primary'):isL1()?button('new-vendor','create2','', 'primary'):'')+'<div class="h-context"><span class="badge info">'+(isRoot()?t('root'):roleName(v))+'</span><span>'+esc(actorName())+'</span><span class="muted">'+t('parent')+' · '+(isRoot()||v.tier===1?t('platform'):esc(vendors.find(x=>x.id===v.parentId).name))+'</span><span class="h-context-period">'+h.from+' — '+h.to+' · Asia/Shanghai</span></div><div class="h-metrics">'+metricCard('unarchivedAccounts',number(allAccounts().filter(a=>!a.archived&&scope.some(v=>v.id===a.vendorId)).length),t(isRoot()?'globalScope':isL1()?'treeScope':'ownOnly'))+metricCard('requests',number(stats.requests))+metricCard('cost',money(stats.cost))+'</div><div class="h-section-heading"><h2>'+t('estimate')+'</h2>'+button('go-finance','finance')+'</div><div class="h-metrics">'+financial+'</div>'+note('twoLegs')+'<div class="h-two-column"><section class="panel h-summary"><h2>'+t('identity')+'</h2>'+detailRows([[t('identity'),isRoot()?t('globalScope'):roleName(v)],[t(isRoot()?'supplierCount':'resources'),isRoot()?String(vendors.length):v.accountLimit+' / '+v.proxyLimit],[t('groups'),isRoot()?Object.values(GROUPS).flat().join(' · '):v.groups.join(' · ')]])+'</section><section class="panel h-summary"><h2>'+t('organization')+'</h2><div class="h-flow"><span>'+(h.actor.role==='vendor_level2'?esc(vendors.find(p=>p.id===v.parentId).name):t('platform'))+'</span>'+scope.filter(x=>x.tier===1||x.id===h.actor.id).slice(0,3).map(x=>'<div><b>'+esc(x.name)+'</b> <span class="badge info">'+roleName(x)+'</span>'+scope.filter(y=>y.parentId===x.id).map(y=>'<p class="h-tree-child">'+esc(y.name)+' '+statusTag(y)+'</p>').join('')+'</div>').join('')+'</div></section></div>';
  }
  function organization() {
    const items=visibleVendors().filter(v=>(isRoot()||v.parentId===h.actor.id)&&(h.status==='all'||stateName(v)===h.status)&&(v.name+' '+v.email).toLowerCase().includes(h.search.toLowerCase()));
    const count=items.length,max=Math.max(1,Math.ceil(count/8));h.currentPage=Math.min(h.currentPage,max);
    return pageHeading(isRoot()?'organization':'children',button('new-vendor',isRoot()?'create1':'create2','', 'primary'))+'<div class="h-metrics">'+metricCard(isRoot()?'tier1':'children',items.filter(v=>isRoot()?v.tier===1:v.parentId===h.actor.id).length)+metricCard('active',items.filter(v=>model.effective(v,vendors)).length)+metricCard('pending',items.filter(v=>v.status==='pending').length)+'</div><section class="panel h-filter"><input id="h-search" aria-label="'+t('search')+'" placeholder="'+t('search')+'" value="'+esc(h.search)+'"><select id="h-status" aria-label="'+t('status')+'">'+options(['all','active','disabled','pending','inherited'].map(k=>[k,t(k==='all'?'anyStatus':k)]),h.status)+'</select><span class="muted">'+t('parentRule')+'</span></section>'+table(['name','status','parent','groups','resources','operation'],items.slice((h.currentPage-1)*8,h.currentPage*8).map(v=>['<strong>'+esc(v.name)+'</strong> <span class="badge info">'+roleName(v)+'</span><small>'+esc(v.email)+'</small>',statusTag(v),v.parentId?esc(vendors.find(x=>x.id===v.parentId).name):t('platform'),v.groups.map(g=>'<span class="h-group">'+esc(g)+'</span>').join(''),'A '+vendorAccounts(v).length+'/'+v.accountLimit+'<small>P '+vendorProxies(v).length+'/'+v.proxyLimit+'</small>','<div class="row">'+button('detail','detail',v.id)+(model.canManage(h.actor,v,vendors)?button('edit','configure',v.id)+((isRoot()?v.tier===1:v.parentId===h.actor.id)?button('contract','contract',v.id):'')+button(v.status==='pending'?'activate':'status',v.status==='pending'?'activation':v.status==='disabled'?'enable':'disable',v.id):'')+'</div>']),'organization')+'<div class="pagination"><span>'+count+' '+t('rows')+'</span><div class="row"><button data-h-action="page-prev" '+(h.currentPage===1?'disabled':'')+'>'+t('prev')+'</button><span>'+h.currentPage+' / '+max+'</span><button data-h-action="page-next" '+(h.currentPage===max?'disabled':'')+'>'+t('nextPage')+'</button></div></div>';
  }
  function openVendor(id) {
    const current=vendors.find(v=>v.id===id);
    if(current&&!model.canManage(h.actor,current,vendors)||!current&&!isRoot()&&!isL1())return notify(t('forbidden'));
    const parent=isL1()?ownerPolicy():null;
    h.edit=current?structuredClone(current):{id:Math.max(...vendors.map(v=>v.id))+1,name:'',email:'',tier:isRoot()?1:2,parentId:parent?.id||null,rootId:parent?.id||Math.max(...vendors.map(v=>v.id))+1,platforms:parent?.platforms||['anthropic','openai'],groups:parent?.groups.slice(0,1)||['Claude 默认'],accountLimit:20,proxyLimit:10,childLimit:20,status:'pending',price:isRoot()?'1.8':'1.6',settlementMultiplier:'1',version:1,created:DEMO_TODAY};
    h.edit.concurrency??=5;h.edit.priority??=1;h.edit.multiplier??=1;h.edit.direct??=false;h.edit.wechat??='';h.edit.settlementEmail??='';h.edit.binanceId??='';h.edit.isNew=!current;h.step=1;renderVendorEditor();
  }
  function editorFirstStep() {
    const v=h.edit,parent=v.parentId?vendors.find(p=>p.id===v.parentId):null,allowed=parent?parent.groups:Object.values(GROUPS).flat();
    const field=(key,label,type='text',value=v[key]??'')=>'<label>'+t(label)+'<input id="h-'+key+'" type="'+type+'" value="'+esc(value)+'"></label>';
    return note('parentRule')+'<div class="h-context"><span class="badge info">'+roleName(v)+'</span><span>'+t('parent')+'：'+(parent?esc(parent.name):t('platform'))+'</span></div><div class="fieldgrid"><label>'+t('name')+'<input id="h-name" maxlength="100" value="'+esc(v.name)+'"></label><label>'+t('email')+'<input id="h-email" type="email" '+(v.isNew?'':'readonly')+' value="'+esc(v.email)+'" placeholder="supplier@example.invalid"></label><label>'+t('accountLimit')+'<input id="h-account-limit" type="number" min="1" max="100000" value="'+v.accountLimit+'"></label><label>'+t('proxyLimit')+'<input id="h-proxy-limit" type="number" min="1" max="100000" value="'+v.proxyLimit+'"></label>'+(v.tier===1?field('childLimit','childCap','number'):'')+'</div><h3>'+t('groups')+'</h3><div class="policy-groups">'+allowed.map(g=>'<label class="checkline"><input name="h-group" type="checkbox" value="'+esc(g)+'" '+(v.groups.includes(g)?'checked':'')+'>'+esc(g)+'</label>').join('')+'</div><details class="form-section"><summary>'+t('defaults')+'</summary><div class="fieldgrid">'+field('concurrency','concurrency','number')+field('priority','priority','number')+field('multiplier','statisticsRate','number')+'</div><label class="checkline"><input id="h-direct" type="checkbox" '+(v.direct?'checked':'')+' '+(parent&&!parent.direct?'disabled':'')+'>'+t('direct')+'</label></details><details class="form-section"><summary>'+t('contact')+'</summary><div class="fieldgrid">'+field('wechat','contact')+field('settlementEmail','settlementEmail','email')+field('binanceId','recipient')+'</div></details>'+note('inviteHelp');
  }
  function priceFields(contract, groups, isNew=false) {
    return '<div class="fieldgrid"><label>'+t('price')+'<input id="h-price" inputmode="decimal" value="'+esc(contract.price)+'"></label><label>'+t('multiplier')+'<input id="h-multiplier" inputmode="decimal" value="'+esc(contract.multiplier)+'"></label><label>'+t('fx')+'<input id="h-fx" inputmode="decimal" value="'+h.fx+'"></label><label>'+t('effective')+'<input id="h-effective" type="datetime-local" value="'+(isNew?'2026-10-15T14:20':'2026-10-16T00:00')+'" '+(isNew?'readonly':'')+'></label></div><details class="form-section"><summary>'+t('groups')+'</summary><div class="fieldgrid">'+groups.map(g=>'<label>'+esc(g)+'<input data-h-rate="'+esc(g)+'" inputmode="decimal" value="'+esc(contract.rates?.[g]??'')+'" placeholder="'+t('price')+'"></label>').join('')+'</div></details>'+note('ratesHelp')+'<div id="h-price-preview" class="h-price-preview"></div>';
  }
  function renderVendorEditor() {
    const v=h.edit;
    openModal(v.isNew?t(v.tier===1?'create1':'create2'):t('configure'),t('parentRule'),'<div class="h-steps"><span class="'+(h.step===1?'active':'')+'">1 · '+t('identityStep')+'</span><span class="'+(h.step===2?'active':'')+'">2 · '+t('priceStep')+'</span></div>'+(h.step===1?editorFirstStep():'<div class="h-context"><strong>'+esc(v.name)+'</strong><span>'+esc(v.email)+'</span><span class="badge info">'+roleName(v)+'</span></div>'+priceFields({price:v.price||'1.6',multiplier:v.settlementMultiplier||'1',rates:v.rates||{}},v.groups,true))+'<p id="h-error" class="form-error" role="alert"></p>',button('dismiss','cancel')+(h.step===1?button('editor-next',v.isNew?'next':'save','', 'primary'):button('editor-back','back')+button('editor-save','createInvite','', 'primary')));
    if(h.step===2)pricePreview();
  }
  function captureIdentity() {
    const v=h.edit;Object.assign(v,{name:$('h-name').value.trim(),email:$('h-email').value.trim(),accountLimit:Number($('h-account-limit').value),proxyLimit:Number($('h-proxy-limit').value),groups:[...document.querySelectorAll('[name=h-group]:checked')].map(e=>e.value)});
    for(const key of ['concurrency','priority','multiplier','childLimit'])if($('h-'+key))v[key]=Number($('h-'+key).value);
    for(const key of ['wechat','settlementEmail','binanceId'])v[key]=$('h-'+key).value.trim();
    v.direct=$('h-direct').checked;
    if(!Number.isInteger(v.concurrency)||v.concurrency<1||v.concurrency>1000||!Number.isInteger(v.priority)||v.priority<0||!Number.isFinite(v.multiplier)||v.multiplier<0||v.multiplier>1000||v.tier===1&&(!Number.isInteger(v.childLimit)||v.childLimit<0||v.childLimit>10000))return 'invalidLimits';
    if(v.parentId&&v.direct&&!vendors.find(p=>p.id===v.parentId).direct)return 'parentSubset';
    if(v.settlementEmail&&!/^[^@\\s]+@example\\.invalid$/i.test(v.settlementEmail)||v.binanceId&&!/^DEMO-[A-Za-z0-9-]+$/.test(v.binanceId))return 'invalidIdentity';
    v.platforms=Object.keys(GROUPS).filter(p=>GROUPS[p].some(g=>v.groups.includes(g)));
    if(!v.name||! /^[^@\s]+@example\.invalid$/i.test(v.email))return 'invalidIdentity';
    if(!v.isNew&&(vendorAccounts(v).length>v.accountLimit||vendorProxies(v).length>v.proxyLimit))return 'invalidLimits';
    return model.validatePolicy(h.actor,v,vendors);
  }
  function readPrice() {
    const price=$('h-price').value.trim(),multiplier=$('h-multiplier').value.trim(),fx=$('h-fx').value.trim(),effective=$('h-effective').value+'+08:00';
    model.decimal(price);model.decimal(multiplier);model.decimal(fx,true);
    if(!Number.isFinite(new Date(effective).getTime())||(h.edit?.isNew?new Date(effective)<new Date(DEMO_NOW):new Date(effective)<=new Date(DEMO_NOW)))throw Error('futureOnly');
    const rates=Object.fromEntries([...document.querySelectorAll('[data-h-rate]')].filter(e=>e.value.trim()).map(e=>[e.dataset.hRate,e.value.trim()]));Object.values(rates).forEach(x=>model.decimal(x));return {price,multiplier,fx,effective,rates};
  }
  function pricePreview() {
    try {const c=readPrice();const target=h.edit||vendors.find(v=>v.id===h.contractTarget);const amount=model.amount(10000000,c.price,c.multiplier,c.fx);const parent=target?.parentId?h.contracts.filter(c=>c.payee===target.parentId).at(-1):null;const income=parent?model.amount(10000000,parent.price,parent.multiplier,c.fx):null;$('h-price-preview').innerHTML='<small>'+t('sample')+'</small><strong>'+cents(amount)+' USDT</strong>'+(income!==null?'<span>'+t('upstream')+' '+cents(income)+' · '+t('margin')+' '+cents(income-amount)+' USDT</span>':'')+(income!==null&&income<amount?note('negative','warning-note'):'');$('h-error').textContent='';} catch(error){$('h-price-preview').innerHTML='';$('h-error').textContent=t(error.message);}
  }
  function saveVendorEditor() {
    const v=h.edit;
    if(v.isNew){let c;try{c=readPrice();}catch(error){$('h-error').textContent=t(error.message);return;}const error=model.validatePolicy(h.actor,v,vendors);if(error){$('h-error').textContent=t(error);return;}
      const clean={...v,userId:2000+v.id,notes:'',coefficient:c.price};delete clean.isNew;vendors.push(clean);h.contracts.push({...c,id:'C-'+v.id+'-1',version:1,payer:v.parentId||0,payee:v.id,leg:v.tier===1?'platform_to_l1':'l1_to_l2'});log('createInvite',v.id,'pending');
    } else {const error=captureIdentity();if(error){$('h-error').textContent=t(error);return;}Object.assign(vendors.find(x=>x.id===v.id),{name:v.name,groups:v.groups,platforms:v.platforms,accountLimit:v.accountLimit,proxyLimit:v.proxyLimit,childLimit:v.childLimit,concurrency:v.concurrency,priority:v.priority,multiplier:v.multiplier,direct:v.direct,wechat:v.wechat,settlementEmail:v.settlementEmail,binanceId:v.binanceId,version:v.version+1});log('configure',v.id,'saved');}
    h.edit=null;$('modal').close();render();notify(t(v.isNew?'invitation':'saved'));
  }
  function accountFilterOptions(vendorId) {
    return [['all',t('allAccountsFilter')],...allAccounts().filter(a=>visibleVendors().some(v=>v.id===a.vendorId)&&(vendorId==='all'||a.vendorId===Number(vendorId)||accountVendor(a).rootId===Number(vendorId))).map(a=>[a.id,a.name+(a.archived?' · '+t('archivedAccount'):'')])];
  }
  function usageControls() {
    const select=(id,label,values,value)=>'<label>'+t(label)+'<select id="'+id+'">'+options(values,value)+'</select></label>';
    return '<section class="panel h-filter">'+select('h-vendor-filter','owner',[['all',t('all')],...visibleVendors().map(v=>[v.id,v.name+' · '+roleName(v)])],h.vendorFilter)
      +select('h-account-filter','accountFilter',accountFilterOptions(h.vendorFilter),h.accountFilter)
      +'<label>'+t('start')+'<input type="date" id="h-from" value="'+h.from+'"></label><label>'+t('end')+'<input type="date" id="h-to" value="'+h.to+'"></label>'
      +(isL1()?select('h-scope','identity',['all','own','downstream'].map(k=>[k,t(k)]),h.scope):'')
      +select('h-dimension','dimension',[['supplier',t('bySupplier')],['account',t('byAccount')],['date',t('byDate')],['group',t('byGroup')],['model',t('byModel')],['account-date',t('accountDate')]],h.dimension)
      +select('h-usage-platform','platform',[['all',t('allPlatforms')],['anthropic','Anthropic'],['openai','OpenAI']],h.usagePlatform)
      +select('h-usage-group','byGroup',[['all',t('allGroups')],...new Set(ownRows().map(r=>r.group))].map(v=>Array.isArray(v)?v:[v,v]),h.usageGroup)
      +select('h-usage-state','currentState',[['all',t('allAccountsFilter')],['active',t('notArchived')],['archived',t('archivedAccount')]],h.usageState)
      +'<label>'+t('reportSearch')+'<input id="h-usage-search" value="'+esc(h.usageSearch)+'" placeholder="'+t('reportSearch')+'"></label>'
      +select('h-usage-sort','sortBy',[['cost',t('reportCost')],['requests',t('requests')]],h.usageSort)
      +select('h-usage-order','sortDirection',['desc','asc'].map(k=>[k,t(k)]),h.usageOrder)
      +button('filter','apply')+'<span class="muted">Asia/Shanghai</span></section>';
  }
  function usageLabel(g) {
    const r=g.items[0];
    if(h.dimension==='supplier')return esc(r.ownerName)+'<small>'+t(r.ownerTier===1?'tier1':'tier2')+' · ID:'+r.leafId+'</small>';
    if(h.dimension==='account'||h.dimension==='account-date'){
      const a=findAccount(r.account);
      return '<strong>'+esc(r.accountName)+'</strong>'+(a?.archived?' <span class="badge archived">'+t('archivedAccount')+'</span>':'')+'<small>#'+r.account+(h.dimension==='account-date'?' · '+r.date:'')+'</small>';
    }
    return esc(g.key);
  }
  function sourceLabel(r) {
    return r.creatorRole?t(r.creatorRole==='super_admin'?'root':r.creatorRole==='vendor_level1'?'tier1':'tier2')+'<small>ID:'+r.creatorId+'</small>':t('unknownSource');
  }
  function reportCells(g, summary=false) {
    const r=g.items[0],s=g.stats,isAccount=['account','account-date'].includes(h.dimension);
    const cells=[summary?t('reportSummary'):usageLabel(g)];
    if(isAccount){
      cells.push(summary?'—':esc(r.ownerName)+'<small>'+t(r.ownerTier===1?'tier1':'tier2')+' · ID:'+r.leafId+'</small>');
      if(h.usageMode==='operations')cells.push(summary?'—':badge(findAccount(r.account)?.health||'未知'),summary?'—':sourceLabel(r));
    }
    const pair=(a,b,ka,kb)=>'<span class="muted">'+t(ka)+'</span> '+number(a)+'<br><span class="muted">'+t(kb)+'</span> '+number(b);
    if(h.usageMode==='operations')cells.push(number(s.requests),pair(s.input,s.output,'inputLabel','outputLabel'),pair(s.write,s.read,'writeLabel','readLabel'),hitRate(s),money(s.cost));
    else cells.push(money(s.opus),money(s.fable),money(s.other),number(s.requests),number(s.totalTokens),money(s.cost));
    return cells;
  }
  function usagePage() {
    const data=rows(),list=model.usageGroups(data,h.dimension,h.usageSort,h.usageOrder),sum=total(data);
    const isAccount=['account','account-date'].includes(h.dimension),max=Math.max(1,Math.ceil(list.length/10));h.currentPage=Math.min(h.currentPage,max);
    const headers=[isAccount?'accountFilter':({supplier:'owner',date:'byDate',model:'byModel',group:'byGroup'})[h.dimension],...(isAccount?['owner',...(h.usageMode==='operations'?['currentHealth','source']:[])]:[]),...(h.usageMode==='operations'?['requests','tokensIO','cacheIO','cacheRate','reportCost']:['opusCost','fableCost','otherCost','requests','tokensWithCache','reportCost'])];
    const totals=reportCells({stats:sum,items:[]},true);
    let markup=table(headers,list.slice((h.currentPage-1)*10,h.currentPage*10).map(g=>reportCells(g)),'usage').replace('admin-table h-table','admin-table h-table h-report-'+h.usageMode);
    markup=markup.replace('</tbody>','</tbody><tfoot><tr>'+totals.map((c,i)=>'<td'+(i?' class="h-number"':'')+'>'+c+'</td>').join('')+'</tr></tfoot>');
    return pageHeading('usage',button('export-usage','export'))+usageControls()
      +'<div class="h-metrics">'+metricCard('requests',number(sum.requests))+metricCard('reportCost',money(sum.cost))+metricCard('accountsCount',new Set(data.map(r=>r.account)).size)+'</div>'
      +'<div class="h-report-tabs" role="group" aria-label="'+t('usage')+'">'+[['operations','operationsView'],['models','modelCostView']].map(([mode,label])=>'<button data-h-action="usage-mode" data-id="'+mode+'" aria-pressed="'+(h.usageMode===mode)+'">'+t(label)+'</button>').join('')+'</div>'
      +'<p class="h-report-note">'+t('reportHelp')+'</p>'+markup+'<div class="pagination"><span>'+list.length+' '+t('rows')+'</span><div class="row"><button data-h-action="page-prev" '+(h.currentPage===1?'disabled':'')+'>'+t('prev')+'</button><span>'+h.currentPage+' / '+max+'</span><button data-h-action="page-next" '+(h.currentPage===max?'disabled':'')+'>'+t('nextPage')+'</button></div></div>'+note('historyHelp');
  }
  function exportUsage() {
    const list=model.usageGroups(rows(),h.dimension,h.usageSort,h.usageOrder),isAccount=['account','account-date'].includes(h.dimension);
    const headers=['dimension','key','account_name','owner_supplier_id','owner_supplier_name','creator_role','creator_id','account_archived','current_health','requests','input_tokens','output_tokens','cache_write_tokens','cache_read_tokens','total_tokens_including_cache','cache_hit_rate','opus_cost_usd','fable_cost_usd','other_cost_usd','usage_cost_usd','from','to','timezone'];
    csvDownload('supplier-v2-usage-demo.csv',headers,list.map(g=>{const r=g.items[0],s=g.stats,a=isAccount?findAccount(r.account):null;return [h.dimension,g.key,isAccount?r.accountName:'',isAccount||h.dimension==='supplier'?r.leafId:'',isAccount||h.dimension==='supplier'?r.ownerName:'',isAccount?r.creatorRole||'unknown':'',isAccount?r.creatorId??'':'',isAccount?!!a?.archived:'',a?.health||'',s.requests,s.input,s.output,s.write,s.read,s.totalTokens,s.cacheHitRate===null?'':s.cacheHitRate.toFixed(6),...[s.opus,s.fable,s.other,s.cost].map(c=>(c/10000).toFixed(4)),h.from,h.to,'Asia/Shanghai'];}));
  }
  function contractsForActor() {return h.contracts.filter(c=>isRoot()?c.payer===0:c.payee===h.actor.id||c.payer===h.actor.id);}
  function openContract(id) {
    const v=vendors.find(v=>v.id===id);
    if(!v||!model.canManage(h.actor,v,vendors))return notify(t('forbidden'));
    // Root configures its own contracts; child price belongs to the level-1 payer.
    if((isRoot()&&v.tier!==1)||(!isRoot()&&v.parentId!==h.actor.id))return notify(t('forbidden'));
    const contract=h.contracts.filter(c=>c.payee===id).at(-1);if(!contract)return;
    h.edit=null;h.contractTarget=id;
    openModal(t('contract')+' · '+v.name,t('twoLegs'),priceFields(contract,v.groups)+'<p id="h-error" class="form-error" role="alert"></p>',button('dismiss','cancel')+button('save-contract','save','', 'primary'));pricePreview();
  }
  function financePage() {
    const tabs=isRoot()?['outgoing','contracts']:isL1()?['incoming','outgoing','contracts']:['incoming','contracts'];
    if(!tabs.includes(h.financeTab))h.financeTab=tabs[0];
    const nav='<div class="admin-tabs" role="tablist" aria-label="'+t('finance')+'">'+tabs.map(key=>'<button role="tab" aria-selected="'+(h.financeTab===key)+'" data-h-action="finance-tab" data-id="'+key+'">'+t(key)+'</button>').join('')+'</div>';
    let content='';
    if(h.financeTab==='contracts') {
      content=note('readContract')+table(['counterparty','identity','price','multiplier','effective','operation'],contractsForActor().map(c=>[esc(vendors.find(v=>v.id===c.payee)?.name),c.leg==='platform_to_l1'?t('platform')+' → '+t('tier1'):t('tier1')+' → '+t('tier2'),esc(c.price)+'<small>v'+c.version+' · '+Object.entries(c.rates).map(([g,p])=>esc(g)+': '+esc(p)).join(' · ')+'</small>',esc(c.multiplier),esc(c.effective.replace('T',' ').replace('+08:00','')),c.payer===(isRoot()?0:h.actor.id)?button('contract','configure',c.payee):'<span class="muted">'+t('readContract')+'</span>']),'contracts');
    } else {
      const list=h.bills.filter(b=>model.canReadBill(h.actor,b)&&(h.financeTab==='outgoing'?b.payer===(isRoot()?0:h.actor.id):b.payee===h.actor.id));
      content='<div class="h-metrics">'+metricCard(h.financeTab==='incoming'?'upstream':isRoot()?'platformPayable':'payable',cents(list.filter(b=>['confirmed','partial','paid'].includes(b.state)).reduce((s,b)=>s+b.cents,0)),'USDT')+metricCard('paid',cents(list.reduce((s,b)=>s+b.paid,0)),'USDT')+'</div>'+table(['counterparty','period','amount','paid','status','operation'],list.map(b=>[b.number+'<small>'+esc(b.payer===0?t('platform'):vendors.find(v=>v.id===b.payer)?.name)+' → '+esc(vendors.find(v=>v.id===b.payee)?.name)+'</small>',b.from+' — '+b.to,cents(b.cents),cents(b.paid),'<span class="badge info">'+t(({review:'reviewBill',paid:'paidState'})[b.state]||b.state)+'</span>',button('bill','detail',b.id)]),'finance');
    }
    return pageHeading('finance',h.financeTab==='outgoing'?button('new-bill','generate','', 'primary'):'')+note('twoLegs')+nav+content;
  }
  function newBill() {
    const payees=isRoot()?vendors.filter(v=>v.tier===1):isL1()?vendors.filter(v=>v.parentId===h.actor.id):[];
    if(!payees.length)return notify(t('forbidden'));
    openModal(t('generate'),t('twoLegs'),'<div class="fieldgrid"><label>'+t('counterparty')+'<select id="h-payee">'+options(payees.map(v=>[v.id,v.name]),payees[0].id)+'</select></label><label>'+t('fx')+'<input id="h-bill-fx" value="'+h.fx+'"></label><label>'+t('start')+'<input id="h-bill-from" type="date" value="'+h.from+'"></label><label>'+t('end')+'<input id="h-bill-to" type="date" value="'+h.to+'"></label></div><div id="h-bill-preview" class="notice"></div><p id="h-error" class="form-error" role="alert"></p>',button('dismiss','cancel')+button('preview-bill','preview')+'<button class="primary" id="h-save-bill" data-h-action="save-bill" disabled>'+t('generate')+'</button>');h.billPreview=null;$('modal').classList.add('wide');previewBill();
  }
  function previewBill() {
    h.billPreview=null;$('h-save-bill').disabled=true;
    try {
      if(!isRoot()&&!isL1())throw Error('forbidden');
      const payee=vendors.find(v=>v.id===Number($('h-payee').value)),leg=isRoot()?'platform_to_l1':'l1_to_l2';
      if(!payee||(isRoot()?payee.tier!==1:payee.parentId!==h.actor.id))throw Error('forbidden');
      const from=$('h-bill-from').value,to=$('h-bill-to').value,fx=$('h-bill-fx').value;
      if(!from||!to||from>to||to>=DEMO_TODAY)throw Error('invalidDates');
      const data=allLedger().filter(r=>r.date>=from&&r.date<=to&&(isRoot()?r.rootId===payee.id:r.leafId===payee.id));
      if(!data.length)throw Error('empty');
      if(h.bills.some(b=>b.state!=='void'&&b.leg===leg&&b.rowIds.some(id=>data.some(r=>r.id===id))))throw Error('overlap');
      const quote=model.quote(data,h.contracts,leg,fx);
      h.billPreview={...quote,from,to,fx,payer:isRoot()?0:h.actor.id,payee:payee.id,leg,rowIds:data.map(r=>r.id),state:'draft',paid:0,notes:[],payments:[]};
      $('h-bill-preview').innerHTML=table(['groups','cost','price','multiplier','amount'],quote.lines.map(l=>[esc(l.group)+'<small>'+esc(l.contract)+'</small>',money(l.cost),esc(l.price),esc(l.multiplier),cents(l.cents)]),'finance')+'<strong>'+t('amount')+' '+cents(quote.cents)+' USDT</strong>';
      $('h-error').textContent='';$('h-save-bill').disabled=false;
    }catch(error){$('h-bill-preview').innerHTML='';$('h-error').textContent=t(error.message);}
  }
  function billDetail(id) {
    const b=h.bills.find(b=>b.id===id);if(!b||!model.canReadBill(h.actor,b))return notify(t('forbidden'));
    const actions=[['send','send'],['confirm','confirmBill'],['dispute','dispute'],['pay','payment'],['void','voidBill']].filter(([action])=>model.transition(h.actor,b,action)!=='forbidden').map(([action,label])=>button('bill-'+action,label,id,action==='void'?'danger':'')).join('');
    openModal(b.number,t(({review:'reviewBill',paid:'paidState'})[b.state]||b.state),note('frozen')+detailRows([[t('counterparty'),esc(b.payer===0?t('platform'):vendors.find(v=>v.id===b.payer).name)+' → '+esc(vendors.find(v=>v.id===b.payee).name)],[t('period'),b.from+' — '+b.to],[t('amount'),cents(b.cents)+' USDT'],[t('paid'),cents(b.paid)+' USDT'],[t('fx'),esc(b.fx)]])+table(['groups','cost','price','multiplier','amount'],b.lines.map(l=>[esc(l.group)+'<small>'+esc(l.contract)+'</small>',money(l.cost),esc(l.price),esc(l.multiplier),cents(l.cents)]),'finance')+(b.notes.length?'<ul>'+b.notes.map(n=>'<li>'+esc(n)+'</li>').join('')+'</ul>':''),button('dismiss','close')+button('bill-export','export',b.id)+actions);$('modal').classList.add('wide');
  }
  function billAction(id,action) {
    const b=h.bills.find(b=>b.id===id);if(!b)return;
    const next=model.transition(h.actor,b,action);if(next==='forbidden')return notify(t('forbidden'));
    if(['void','dispute','pay'].includes(action)) {
      openModal(t(action==='pay'?'payment':action==='void'?'voidBill':'dispute'),b.number,action==='pay'?'<div class="fieldgrid"><label>'+t('amount')+'<input id="h-payment" inputmode="decimal" value="'+((b.cents-b.paid)/100).toFixed(2)+'"></label><label>'+t('reference')+'<input id="h-reference" maxlength="100" placeholder="DEMO-PAY-001"></label></div><p id="h-error" class="form-error"></p>':'<label>'+t('reason')+'<textarea id="h-reason" maxlength="300"></textarea></label><p id="h-error" class="form-error"></p>',button('dismiss','cancel')+button('bill-commit-'+action,'confirm',b.id,'primary'));return;
    }
    b.state=next;log(action,b.payee,b.number);render();billDetail(id);
  }
  function openResourceList(kind, vendorId) {
    const owner=vendors.find(v=>v.id===vendorId);
    if(!owner||!isRoot()&&(!isL1()||owner.parentId!==h.actor.id||owner.rootId!==h.actor.id))return notify(t('forbidden'));
    if(isRoot()){
      adminFilters=defaultAdminFilters();adminFilters.vendor=String(vendorId);adminPages={};resetProxyWorkspace();navigate('admin-'+kind);
    }else{
      h.resourceVendor=String(vendorId);h.resourceSearch='';h.resourceState='all';h.resourceType='all';navigate('child-'+kind);
    }
  }
  function childResourcePage(kind) {
    if(!isL1())return note('forbidden');
    const owners=vendors.filter(v=>v.tier===2&&v.parentId===h.actor.id&&v.rootId===h.actor.id);
    const isAccount=kind==='accounts', title=isAccount?'childAccounts':'childProxies';
    const data=(isAccount?allAccounts():allProxies()).filter(r=>owners.some(v=>v.id===r.vendorId)&&model.resourceAccess(h.actor,r,vendors).read);
    const search=h.resourceSearch.trim().toLowerCase();
    const items=data.filter(r=>(h.resourceVendor==='all'||r.vendorId===Number(h.resourceVendor))&&(!search||[r.name,r.id,isAccount?'':r.host].join(' ').toLowerCase().includes(search))&&(h.resourceType==='all'||(isAccount?r.platform:r.protocol)===h.resourceType)&&(h.resourceState==='all'||(isAccount?(h.resourceState==='archived')===!!r.archived:proxyStatus(r)===h.resourceState))).sort((a,b)=>b.id-a.id);
    const max=Math.max(1,Math.ceil(items.length/10));h.currentPage=Math.min(h.currentPage,max);
    const ownerCell=r=>esc(vendors.find(v=>v.id===r.vendorId).name)+'<small>'+t('tier2')+' · ID:'+r.vendorId+'</small>';
    const controls='<section class="panel h-filter"><input id="h-resource-search" aria-label="'+t('resourceSearch')+'" placeholder="'+t('resourceSearch')+'" value="'+esc(h.resourceSearch)+'"><label>'+t('owner')+'<select id="h-resource-vendor">'+options([['all',t('allChildren')],...owners.map(v=>[v.id,v.name])],h.resourceVendor)+'</select></label><label>'+t('resourceType')+'<select id="h-resource-type">'+options([['all',t('anyType')],...(isAccount?[['anthropic','Anthropic'],['openai','OpenAI']]:['HTTP','HTTPS','SOCKS5','SOCKS5H'].map(v=>[v,v]))],h.resourceType)+'</select></label><label>'+t('status')+'<select id="h-resource-state">'+options([['all',t('anyStatus')],...(isAccount?[['active',t('notArchived')],['archived',t('archivedAccount')]]:['可用','异常','未测试','已停用','已过期'].map(v=>[v,v]))],h.resourceState)+'</select></label></section>';
    const cells=items.slice((h.currentPage-1)*10,h.currentPage*10).map(r=>isAccount?[
      '<strong>'+esc(r.name)+'</strong><small>#'+r.id+'</small>'+(r.archived?'<span class="badge archived">'+t('archivedAccount')+'</span>':''),ownerCell(r),platformName(r.platform),badge(status(r))+(r.archived?'<small>'+t('archivedStop')+'</small>':''),r.groups.map(g=>'<span class="h-group">'+esc(g)+'</span>').join(''),'<div class="quota-cell">'+quotaCell(r)+'</div>',actionButton('detail',t('detail'),r.id)+actionButton('usage-detail',t('usage'),r.id)
    ]:[
      '<strong>'+esc(r.name)+'</strong><small>#'+r.id+' · '+esc(r.protocol)+'</small>',ownerCell(r),'<code>'+esc(r.host)+':'+r.port+'</code>',esc(regionLabel(r.country)+(r.city?' · '+r.city:'')),badge(proxyStatus(r)),String(proxyBindings(r.id).length),esc(r.created||'—'),actionButton('proxy-detail',t('detail'),r.id)
    ]);
    return pageHeading(title,'<span class="badge info">'+t('readOnly')+'</span>')+note('childReadHelp')+controls+table(isAccount?['name','owner','platform','status','groups','quotaLabel','operation']:['name','owner','endpoint','location','status','bindings','createdAt','operation'],cells,title)+'<div class="pagination"><span>'+items.length+' '+t('rows')+'</span><div class="row"><button data-h-action="page-prev" '+(h.currentPage===1?'disabled':'')+'>'+t('prev')+'</button><span>'+h.currentPage+' / '+max+'</span><button data-h-action="page-next" '+(h.currentPage===max?'disabled':'')+'>'+t('nextPage')+'</button></div></div>';
  }
  function showVendorDetail(id) {
    const v=visibleVendors().find(x=>x.id===id);if(!v)return notify(t('forbidden'));
    openModal(v.name,roleName(v),detailRows([[t('email'),esc(v.email)],[t('parent'),v.parentId?esc(vendors.find(p=>p.id===v.parentId).name):t('platform')],[t('status'),statusTag(v)],[t('groups'),v.groups.map(esc).join(' · ')],[t('resources'),'A '+vendorAccounts(v).length+'/'+v.accountLimit+' · P '+vendorProxies(v).length+'/'+v.proxyLimit]])+note('preserved'),button('dismiss','close')+button('resource-accounts','pool',v.id)+button('resource-proxies','allProxies',v.id));
  }
  function renderAudit() {
    const list=h.events.filter(e=>isRoot()||visibleVendors().some(v=>v.id===e.target));
    return pageHeading('audit')+table(['time','actor','counterparty','operation','result'],list.map(e=>[e.at,esc(e.actor),esc(vendors.find(v=>v.id===e.target)?.name||''),t(e.action),t(e.result)]),'audit');
  }
  const legacyAdmin=renderAdmin,legacyScopeBlock=vendorScopeBlock;
  profileIdentity=()=>({name:actorName(),email:isRoot()?'super-admin@example.invalid':isSupplier()?ownerPolicy().email:'admin@example.invalid',role:isRoot()?t('root'):isSupplier()?roleName(ownerPolicy()):t('admin')});
  renderProfileCenter=()=>{
    const identity=profileIdentity();$('profile-avatar').textContent=identity.name.slice(0,2);$('profile-display-name').textContent=identity.name;$('profile-role').textContent=identity.role;
    $('profile-trigger').setAttribute('aria-label',t('accountSettings'));$('profile-trigger').setAttribute('aria-expanded','false');
    $('profile-menu').innerHTML='<div class="profile-menu-head"><span class="avatar large">'+esc(identity.name.slice(0,2))+'</span><div><strong>'+esc(identity.name)+'</strong><small>'+esc(identity.email)+'</small><span class="profile-role-badge">'+esc(identity.role)+'</span></div></div><button role="menuitem" data-action="profile-settings">'+icon('users')+t('accountSettings')+'</button><button role="menuitem" class="danger" data-action="profile-logout">'+t('logout')+'</button>';$('profile-menu').hidden=true;
  };
  openProfileSettings=(tab='info')=>{
    profileSettingsTab=tab;const identity=profileIdentity();
    const tabs='<div class="account-settings-tabs">'+['info','password'].map(key=>'<button data-action="profile-settings-tab" data-tab="'+key+'" class="'+(tab===key?'active':'')+'">'+t(key)+'</button>').join('')+'</div>';
    const fields=tab==='info'?'<div class="profile-card"><span class="avatar large">'+esc(identity.name.slice(0,2))+'</span><div><strong>'+esc(identity.email)+'</strong><span class="profile-role-badge">'+esc(identity.role)+'</span><small>'+esc(identity.name)+'</small></div></div>'+detailRows([[t('email'),esc(identity.email)],[t('identity'),esc(identity.role)],[t('parent'),isRoot()||ownerPolicy().tier===1?t('platform'):esc(vendors.find(v=>v.id===ownerPolicy().parentId).name)],[t('registered'),'2026/10/12 20:22']]):'<div class="settings-form">'+[['current','currentPassword'],['new','newPassword'],['confirm','confirmPassword']].map(([key,label])=>'<label>'+t(label)+'<input id="profile-'+key+'-password" type="password" autocomplete="'+(key==='current'?'current-password':'new-password')+'"></label>').join('')+note('demo')+'<p id="profile-password-error" class="form-error"></p></div>';
    openModal(t('accountSettings'),t('identity'),tabs+fields,button('dismiss','close')+(tab==='password'?'<button class="primary" data-h-action="profile-password">'+t('save')+'</button>':''));
  };
  vendorScopeBlock=a=>{const v=accountVendor(a);if(!model.effective(v,vendors))return t(v.status==='active'?'inherited':v.status==='pending'?'pending':'disabled');return legacyScopeBlock(a);};
  vendorPolicyForm=id=>openVendor(id);
  showVendor=id=>showVendorDetail(id);
  confirmVendorStatus=id=>requestStatus(id);
  renderAdmin=()=>{
    if(['vendors','usage','settlements','audit'].includes(adminView)){h.page=({vendors:'organization',usage:'h-usage',settlements:'h-finance',audit:'h-audit'})[adminView];render();return;}
    renderResourceAdmin();
  };
  function renderResourceAdmin() {
    legacyAdmin();
    $('view').querySelector('.heading')?.remove();
    $('view').querySelector('.plain-stats')?.remove();
    $('view').querySelector('.admin-tabs')?.remove();
    $('view').insertAdjacentHTML('afterbegin',pageHeading(adminView==='accounts'?'allAccounts':adminView==='proxies'?'allProxies':'supplyGroups'));
  }
  function requestStatus(id) {
    const v=vendors.find(v=>v.id===id);if(!v||!model.canManage(h.actor,v,vendors))return notify(t('forbidden'));
    openModal(t(v.status==='disabled'?'enable':'disable'),v.name,note('preserved'),button('dismiss','cancel')+button('status-confirm','confirm',id,v.status==='active'?'danger':'primary'));
  }
  function navigate(target) {
    if(target!==h.page&&['child-accounts','child-proxies'].includes(target)){h.resourceType='all';h.resourceState='all';h.resourceSearch='';}
    h.page=target;h.currentPage=1;h.search='';h.status='all';$('modal').close();setPreviewNavigation(false);render();
  }
  function closePreferences(focus=false) {
    for(const key of ['theme','language']){const menu=$('h-'+key+'-menu'),trigger=$('h-'+key+'-trigger');if(menu&&!menu.hidden){menu.hidden=true;trigger?.setAttribute('aria-expanded','false');if(focus)trigger?.focus();}}
  }
  render=()=>{
    if(portalMode&&!portalLoggedIn){navigation();renderPortalLogin();document.querySelector('.topbar').hidden=false;document.querySelector('.profile-wrap').hidden=true;$('reset').hidden=true;applyTheme();return;}
    document.querySelector('.profile-wrap').hidden=false;$('reset').hidden=portalMode;$('portal-logout').hidden=true;
    page=h.page;
    document.querySelector('.sidebar').hidden=false;document.querySelector('.topbar').hidden=false;
    document.querySelector('.app').classList.remove('portal-layout');
    const blocked=isSupplier()&&!model.effective(ownerPolicy(),vendors);
    if(h.actor.role==='admin'){$('view').innerHTML=pageHeading('organization')+note('noPermission','danger-note');}
    else if(blocked){$('view').innerHTML=pageHeading('identity')+statusTag(ownerPolicy())+note('preserved');}
    else if(['accounts','proxies'].includes(h.page)) {
      // V1 resource workbenches keep their proven interactions; the active fixture is scoped above.
      if(h.page==='accounts')renderAccounts();else renderProxies();
    }else if(['child-accounts','child-proxies'].includes(h.page)) {
      $('view').innerHTML=childResourcePage(h.page==='child-accounts'?'accounts':'proxies');
    }else if(['admin-accounts','admin-proxies','admin-groups'].includes(h.page)) {
      if(!isRoot()){$('view').innerHTML=note('forbidden');}
      else{page='admin';adminView=h.page==='admin-accounts'?'accounts':h.page==='admin-proxies'?'proxies':'groups';renderResourceAdmin();}
    }else {
      const renderers={overview:dashboard,organization,children:organization,'h-usage':usagePage,'h-finance':financePage,'h-audit':renderAudit};
      if((h.page==='organization'&&!isRoot())||(h.page==='children'&&!isL1()))$('view').innerHTML=note('forbidden');
      else $('view').innerHTML=(renderers[h.page]||dashboard)();
    }
    navigation();applyTheme();setPreviewNavigation(false);
  };
  function handleAction(action,id,event) {
    if(action==='resource-accounts'||action==='resource-proxies'){openResourceList(action.slice('resource-'.length),Number(id));return;}
    if(action==='profile-password'){if(!$('profile-current-password').value||$('profile-new-password').value.length<12||$('profile-new-password').value!==$('profile-confirm-password').value){$('profile-password-error').textContent=t('passwordError');return;}$('modal').close();notify(t('demo'));return;}
    if(action==='dismiss'){$('modal').close();h.edit=null;return;}
    if(action==='language-menu'||action==='theme-menu'){
      const key=action==='theme-menu'?'theme':'language',menu=$('h-'+key+'-menu'),wasOpen=!menu.hidden;closePreferences();menu.hidden=wasOpen;$('h-'+key+'-trigger').setAttribute('aria-expanded',String(!wasOpen));if(!wasOpen)menu.querySelector('[aria-checked=true]')?.focus();return;
    }
    if(action==='locale'){h.locale=id;setPreference('vendor-prototype-locale',id);render();return;}
    if(action==='theme'){h.theme=id;applyTheme();navigation();$('h-theme-trigger').focus();return;}
    if(action==='new-vendor'){openVendor();return;}
    if(action==='edit'){openVendor(Number(id));return;}
    if(action==='detail'){showVendorDetail(Number(id));return;}
    if(action==='status'){requestStatus(Number(id));return;}
    if(action==='status-confirm'||action==='activate'){
      const v=vendors.find(v=>v.id===Number(id));if(!v||!model.canManage(h.actor,v,vendors))return notify(t('forbidden'));
      if(action==='activate'&&v.status!=='pending')return;
      v.status=action==='activate'?'active':v.status==='active'?'disabled':'active';v.version++;log(action==='activate'?'activation':'status',v.id,v.status);$('modal').close();render();return;
    }
    if(action==='editor-next'){const error=captureIdentity();if(error){$('h-error').textContent=t(error);return;}if(!h.edit.isNew){saveVendorEditor();return;}h.step=2;renderVendorEditor();return;}
    if(action==='editor-back'){try{const c=readPrice();h.edit.price=c.price;h.edit.settlementMultiplier=c.multiplier;h.edit.rates=c.rates;}catch{}h.step=1;renderVendorEditor();return;}
    if(action==='editor-save'){saveVendorEditor();return;}
    if(action==='contract'){openContract(Number(id));return;}
    if(action==='save-contract'){
      try{const v=vendors.find(v=>v.id===h.contractTarget);if(!model.canManage(h.actor,v,vendors)||(isRoot()?v.tier!==1:v.parentId!==h.actor.id))throw Error('forbidden');const c=readPrice(),versions=h.contracts.filter(c=>c.payee===v.id),latest=versions.at(-1);if(new Date(c.effective)<=new Date(latest.effective))throw Error('futureOnly');h.contracts.push({...c,id:'C-'+v.id+'-'+(latest.version+1),version:latest.version+1,payer:v.parentId||0,payee:v.id,leg:v.tier===1?'platform_to_l1':'l1_to_l2'});log('contract',v.id,'saved');$('modal').close();render();notify(t('saved'));}catch(error){$('h-error').textContent=t(error.message);}return;
    }
    if(action==='go-finance'){navigate('h-finance');return;}
    if(action==='page-prev'||action==='page-next'){h.currentPage+=action==='page-prev'?-1:1;h.currentPage=Math.max(1,h.currentPage);render();return;}
    if(action==='filter'){
      const from=$('h-from').value,to=$('h-to').value;if(!from||!to||from>to||to>DEMO_TODAY){notify(t('invalidDates'));return;}h.from=from;h.to=to;h.scope=$('h-scope')?.value||'all';h.vendorFilter=$('h-vendor-filter').value;h.accountFilter=$('h-account-filter').value;h.dimension=$('h-dimension').value;h.usagePlatform=$('h-usage-platform').value;h.usageGroup=$('h-usage-group').value;h.usageState=$('h-usage-state').value;h.usageSearch=$('h-usage-search').value.trim();h.usageSort=$('h-usage-sort').value;h.usageOrder=$('h-usage-order').value;h.currentPage=1;render();return;
    }
    if(action==='usage-mode'){h.usageMode=id;render();return;}
    if(action==='export-usage'){exportUsage();return;}
    if(action==='finance-tab'){h.financeTab=id;render();return;}
    if(action==='new-bill'){newBill();return;}
    if(action==='preview-bill'){previewBill();return;}
    if(action==='save-bill'){previewBill();if(!h.billPreview)return;const b=structuredClone(h.billPreview);b.id=h.bills.length+1;b.number=(b.leg==='platform_to_l1'?'PL-':'LC-')+String(b.id).padStart(4,'0');h.bills.push(b);log('generate',b.payee,b.number);$('modal').close();render();billDetail(b.id);return;}
    if(action==='bill'){billDetail(Number(id));return;}
    if(action==='bill-export'){const b=h.bills.find(b=>b.id===Number(id));if(!b||!model.canReadBill(h.actor,b))return;csvDownload(b.number+'-demo.csv',['statement','leg','payer','payee','group','contract','basis_usd','price','multiplier','fx','usdt'],b.lines.map(l=>[b.number,b.leg,b.payer,b.payee,l.group,l.contract,(l.cost/10000).toFixed(4),l.price,l.multiplier,b.fx,(l.cents/100).toFixed(2)]));return;}
    if(action?.startsWith('bill-commit-')){
      const op=action.slice('bill-commit-'.length),b=h.bills.find(b=>b.id===Number(id));if(!b||model.transition(h.actor,b,op)==='forbidden')return notify(t('forbidden'));
      if(op==='pay'){
        const raw=$('h-payment').value,reference=$('h-reference').value.trim();if(!/^(0|[1-9]\d{0,10})(\.\d{1,2})?$/.test(raw)){ $('h-error').textContent=t('invalidPayment');return;}
        const [whole,fraction='']=raw.split('.'),value=Number(whole)*100+Number(fraction.padEnd(2,'0'));
        if(value<=0||value>b.cents-b.paid||!reference||b.payments.some(p=>p.reference===reference)){$('h-error').textContent=t('invalidPayment');return;}
        b.paid+=value;b.state=b.paid===b.cents?'paid':'partial';b.payments.push({reference,cents:value,at:DEMO_NOW});b.notes.push(reference+' · '+cents(value)+' USDT');
      }else{const reason=$('h-reason').value.trim();if(!reason){$('h-error').textContent=t('reason');return;}b.state=model.transition(h.actor,b,op);b.notes.push(reason);}
      log(op,b.payee,b.number);render();billDetail(b.id);return;
    }
    if(action?.startsWith('bill-'))billAction(Number(id),action.slice(5));
  }
  document.addEventListener('click',event=>{
    const target=event.target.closest('button');
    const legacyAction=target?.dataset.action, resourceID=Number(target?.dataset.id);
    const accountReads=['detail','usage-detail','admin-usage-detail'];
    const accountWrites=['edit','reauth','probe','sync-account','toggle','delete-account','confirm-delete','scoped-edit','admin-account-edit','admin-account-save','export-account','account-menu'];
    if(accountReads.includes(legacyAction)||accountWrites.includes(legacyAction)){
      const access=accountAccess(findAccount(resourceID));
      if(!access[accountReads.includes(legacyAction)?'read':'manage']){event.stopImmediatePropagation();event.preventDefault();notify(t('forbidden'));return;}
    }
    if(legacyAction==='proxy-detail'&&!canReadProxy(allProxies().find(p=>p.id===resourceID))){event.stopImmediatePropagation();notify(t('forbidden'));return;}
    if(target?.dataset.action==='portal-login'){
      event.stopImmediatePropagation();
      const v=vendors.find(v=>v.email.toLowerCase()===$('portal-email').value.trim().toLowerCase());
      if(!v||!model.effective(v,vendors)){$('portal-login-error').textContent=t('forbidden');return;}
      portalLoggedIn=true;setActor('vendor_level'+v.tier+':'+v.id);return;
    }
    if(target?.id==='reset'){event.stopImmediatePropagation();location.reload();return;}
    if(target?.dataset.hPage){event.stopImmediatePropagation();navigate(target.dataset.hPage);return;}
    if(target?.dataset.hAction){event.stopImmediatePropagation();if(target.disabled)return;handleAction(target.dataset.hAction,target.dataset.id,event);return;}
    if(!event.target.closest('.h-preference'))closePreferences();
  },true);
  document.addEventListener('change',event=>{
    const e=event.target;
    const resourceKey={'h-resource-vendor':'resourceVendor','h-resource-state':'resourceState','h-resource-type':'resourceType'}[e.id];
    if(resourceKey){h[resourceKey]=e.value;h.currentPage=1;render();return;}
    if(e.id==='preview-role'){event.stopImmediatePropagation();setActor(e.value);}
    if(e.id==='h-vendor-filter'){$('h-account-filter').innerHTML=options(accountFilterOptions(e.value),'all');}
    if(e.id==='h-status'){h.status=e.value;h.currentPage=1;render();}
    if(['h-payee','h-bill-from','h-bill-to','h-bill-fx'].includes(e.id)){h.billPreview=null;$('h-save-bill').disabled=true;$('h-bill-preview').innerHTML='';}
  },true);
  document.addEventListener('input',event=>{
    const e=event.target;
    if(e.id==='h-resource-search'){const pos=e.selectionStart;h.resourceSearch=e.value;h.currentPage=1;render();$('h-resource-search').focus();$('h-resource-search').setSelectionRange(pos,pos);return;}
    if(e.id==='h-search'){const pos=e.selectionStart;h.search=e.value;h.currentPage=1;render();$('h-search').focus();$('h-search').setSelectionRange(pos,pos);}
    if(['h-price','h-multiplier','h-fx','h-effective'].includes(e.id)||e.dataset.hRate)pricePreview();
    if(['h-bill-from','h-bill-to','h-bill-fx'].includes(e.id)){h.billPreview=null;$('h-save-bill').disabled=true;}
  });
  document.addEventListener('keydown',event=>{
    if(event.key==='Escape')closePreferences(true);
    const menu=event.target.closest('.h-menu');if(!menu)return;
    if(['ArrowDown','ArrowUp','Home','End'].includes(event.key)){
      event.preventDefault();const items=[...menu.querySelectorAll('button')],index=items.indexOf(event.target);
      items[event.key==='Home'?0:event.key==='End'?items.length-1:(index+(event.key==='ArrowUp'?-1:1)+items.length)%items.length]?.focus();
    }
  });
  Object.assign(words, {
    proxyConnectLabel: ['测试连接', 'Test connection', '測試連線', '接続テスト'],
    proxySearchLabel: ['搜索代理', 'Search proxies', '搜尋代理', 'プロキシを検索'],
    proxySelectPageLabel: ['选择当前页代理', 'Select proxies on this page', '選擇本頁代理', 'このページのプロキシを選択'],
    proxyPageSizeLabel: ['代理每页条数', 'Proxies per page', '每頁代理筆數', '1ページのプロキシ件数'],
    proxyOverviewLabel: ['代理概览', 'Proxy overview', '代理概覽', 'プロキシ概要'],
    proxyOwnTableLabel: ['我的代理工作台', 'My proxy workbench', '我的代理工作台', '自分のプロキシ一覧'],
    proxyAdminTableLabel: ['供应商代理工作台', 'Supplier proxy workbench', '供應商代理工作台', 'サプライヤーのプロキシ一覧'],
    ux0: ["一键测试", "Test all", "一鍵測試", "一括テスト"],
    ux1: ["测试已选", "Test selected", "測試已選", "選択項目をテスト"],
    ux2: ["测试中", "Testing", "測試中", "テスト中"],
    ux3: ["停止测试", "Stop testing", "停止測試", "テストを停止"],
    ux4: ["正在停止…", "Stopping…", "正在停止…", "停止中…"],
    ux5: ["选择代理 {id}", "Select proxy {id}", "選擇代理 {id}", "プロキシ {id} を選択"],
    ux6: ["复制代理 {id} 地址", "Copy proxy {id} address", "複製代理 {id} 位址", "プロキシ {id} のアドレスをコピー"],
    ux7: ["复制代理地址（不含认证）", "Copy proxy address (without credentials)", "複製代理位址（不含認證）", "プロキシアドレスをコピー（認証情報なし）"],
    ux8: ["测试范围：已选 {count} 个代理（可跨页）", "Scope: {count} selected proxies (across pages)", "測試範圍：已選 {count} 個代理（可跨頁）", "対象：選択した {count} 件（ページをまたぐ）"],
    ux9: ["测试范围：当前筛选全部 {count} 个代理（含其他页）", "Scope: all {count} proxies matching filters (all pages)", "測試範圍：目前篩選全部 {count} 個代理（含其他頁）", "対象：現在の条件に一致する全 {count} 件（全ページ）"],
    ux10: ["正在测试 {done} / {count} 个代理", "Testing {done} / {count} proxies", "正在測試 {done} / {count} 個代理", "テスト中 {done} / {count} 件"],
    ux11: ["测试完成：{valid} 个有效，{invalid} 个无效，{skipped} 个跳过", "Test complete: {valid} valid, {invalid} invalid, {skipped} skipped", "測試完成：{valid} 個有效，{invalid} 個無效，{skipped} 個略過", "完了：有効 {valid} 件、無効 {invalid} 件、スキップ {skipped} 件"],
    ux12: ["测试已停止：{valid} 个有效，{invalid} 个无效，{skipped} 个跳过，{cancelled} 个未执行", "Test stopped: {valid} valid, {invalid} invalid, {skipped} skipped, {cancelled} not run", "測試已停止：{valid} 個有效，{invalid} 個無效，{skipped} 個略過，{cancelled} 個未執行", "停止：有効 {valid} 件、無効 {invalid} 件、スキップ {skipped} 件、未実行 {cancelled} 件"],
    ux13: ["代理可用，延迟 {latency} ms", "Proxy available, latency {latency} ms", "代理可用，延遲 {latency} ms", "プロキシ利用可能、遅延 {latency} ms"],
    ux14: ["连接超时，账号授权保留", "Connection timed out; account authorization retained", "連線逾時，帳號授權保留", "接続タイムアウト。アカウント認証は保持"],
    ux15: ["停用或过期的代理不执行测试", "Disabled or expired proxies are skipped", "停用或過期的代理不執行測試", "無効または期限切れのプロキシはスキップ"],
    ux16: ["当前范围没有可检测代理", "No proxies to test in this scope", "目前範圍沒有可測試代理", "対象のプロキシがありません"],
    ux17: ["本次检测 {count} 个代理，停用或过期项跳过，不自动启用。", "Test {count} proxies. Disabled or expired items are skipped and stay disabled.", "本次測試 {count} 個代理，停用或過期項略過，不自動啟用。", "{count} 件をテスト。無効・期限切れはスキップし、有効化しません。"],
    ux18: ["配置已变更，请重测", "Configuration changed; test again", "設定已變更，請重測", "設定が変更されました。再テストしてください"],
    ux19: ["对象已不存在", "Proxy no longer exists", "代理已不存在", "プロキシは存在しません"],
    ux20: ["未应用检测结果", "Test result was not applied", "未套用測試結果", "テスト結果は適用されませんでした"],
    ux21: ["TLS / Claude / OpenAI 可达", "TLS / Claude / OpenAI reachable", "TLS / Claude / OpenAI 可連線", "TLS / Claude / OpenAI に接続可能"],
    ux22: ["已复制无认证信息的代理地址", "Proxy address copied without credentials", "已複製不含認證資訊的代理位址", "認証情報を含まないアドレスをコピーしました"],
    ux23: ["刷新代理", "Refresh proxies", "重新整理代理", "プロキシを更新"],
    ux24: ["总代理数", "Total proxies", "代理總數", "プロキシ総数"],
    ux25: ["占用中异常 / 不可用", "In-use invalid / unavailable", "使用中異常 / 不可用", "使用中の異常・利用不可"],
    ux26: ["占用", "In use", "使用中", "使用中"],
    ux27: ["占用账号", "Bound accounts", "綁定帳號", "関連アカウント"],
    ux28: ["供应商专属", "Private to supplier", "供應商專屬", "サプライヤー専用"],
    ux29: ["创建者", "Creator", "建立者", "作成者"],
    ux30: ["导入 / 创建时间", "Imported / created", "匯入 / 建立時間", "インポート・作成日時"],
    ux31: ["共享范围", "Sharing scope", "共用範圍", "共有範囲"],
    ux32: ["全部地区", "All regions", "全部地區", "すべての地域"],
    ux33: ["使用状态", "Usage state", "使用狀態", "利用状態"],
    ux34: ["管理我的专属出口、占用情况与可用状态", "Manage private endpoints, bindings and availability", "管理我的專屬出口、綁定與可用狀態", "専用接続先、関連付けと利用状況を管理"],
    ux35: ["仅刷新列表，不触发检测", "Refresh list without running tests", "僅重新整理清單，不執行測試", "テストせず一覧を更新"],
    ux36: ["列表已刷新；没有触发检测。", "List refreshed; no tests were run.", "清單已更新；未執行測試。", "一覧を更新しました。テストは実行していません。"],
    ux37: ["已刷新演示快照", "Demo snapshot refreshed", "演示快照已更新", "デモのスナップショットを更新"],
    ux38: ["占用数为绑定账号数，不是并发或限额。专属代理只在同一供应商内使用；认证、有效期、质量和引用在详情中查看。修改出口保留账号授权。", "In use counts bound accounts. Private proxies belong to one supplier. Details include authentication state, expiry, quality and references. Changing an endpoint retains account authorization.", "使用數為綁定帳號數。專屬代理僅供同一供應商使用；詳情包含認證狀態、效期、品質與引用。修改出口保留帳號授權。", "使用数は関連アカウント数です。専用プロキシは同じサプライヤーのみ利用可能。認証状態、期限、品質、参照は詳細で確認できます。接続先を変更してもアカウント認証は保持されます。"],
    ux39: ["未勾选时，测试与导出作用于当前筛选的全部结果", "With no selection, tests and exports cover all filtered results", "未勾選時，測試與匯出涵蓋目前篩選全部結果", "未選択時は全検索結果がテスト・エクスポート対象"],
    ux40: ["没有符合条件的代理。调整筛选或添加自己的代理。", "No matching proxies. Adjust filters or add a proxy.", "沒有符合條件的代理。請調整篩選或新增代理。", "条件に一致するプロキシがありません。条件を変更するか追加してください。"],
    ux41: ["导入 / 创建时间（起）", "Imported / created from", "匯入 / 建立時間（起）", "インポート・作成日（開始）"],
    ux42: ["应用日期", "Apply dates", "套用日期", "日付を適用"],
    ux43: ["按协议排序", "Sort by protocol", "依協定排序", "プロトコル順"],
    ux44: ["按有效期排序", "Sort by expiry", "依效期排序", "有効期限順"],
    ux45: ["未检测", "Not tested", "未測試", "未テスト"],
    ux46: ["演示检测", "Demo check", "演示測試", "デモテスト"],
    ux47: ["手动标签", "Manual label", "手動標籤", "手動ラベル"],
    ux48: ["供应商本人", "Supplier", "供應商本人", "サプライヤー本人"],
    ux49: ["账号 ID / 平台", "Account ID / platform", "帳號 ID / 平台", "アカウントID・プラットフォーム"],
    ux50: ["账号详情不包含授权凭证", "Account details do not include credentials", "帳號詳情不包含授權憑證", "アカウント詳細に認証情報は含まれません"],
    ux51: ["创建来源", "Created by", "建立來源", "作成元"],
    ux52: ["未记录创建来源", "Creator not recorded", "未記錄建立來源", "作成者の記録なし"],
    ux53: ["并发 / 优先级", "Concurrency / priority", "並行數 / 優先順序", "同時実行数・優先度"],
    ux54: ["总请求数 / Token", "Total requests / tokens", "總請求數 / Token", "総リクエスト・トークン"],
    ux55: ["累计统计消耗", "Lifetime usage cost", "累計統計消耗", "累計使用コスト"],
    ux56: ["归档时间", "Archived at", "封存時間", "アーカイブ日時"],
    ux57: ["账号级冷却", "Account cooldown", "帳號冷卻", "アカウントのクールダウン"],
    ux58: ["当前没有冷却", "No active cooldown", "目前沒有冷卻", "クールダウンなし"],
    ux59: ["模型级限制", "Model restrictions", "模型限制", "モデル制限"],
    ux60: ["无已知模型级限制", "No known model restrictions", "無已知模型限制", "既知のモデル制限なし"],
    ux61: ["最近 60 秒速率", "Rate in the last 60 seconds", "最近 60 秒速率", "直近60秒のレート"],
    ux62: ["当前不可调度", "Not schedulable", "目前不可排程", "スケジュール不可"],
    ux63: ["可调度", "Schedulable", "可排程", "スケジュール可能"],
    ux64: ["当前状态", "Current status", "目前狀態", "現在の状態"],
    ux65: ["调度状态在打开时获取，不自动刷新。当前为虚构演示快照。", "Scheduling status is a snapshot taken when opened; it does not auto-refresh. Demo data.", "排程狀態在開啟時取得，不自動更新。目前為虛構演示快照。", "開いた時点のスケジュール状態です。自動更新されません。デモデータです。"],
    ux66: ["账号、供应商及出口条件均满足演示调度资格。", "Account, supplier and endpoint meet the demo scheduling conditions.", "帳號、供應商及出口均符合演示排程條件。", "アカウント、サプライヤー、接続先はデモのスケジュール条件を満たしています。"],
    ux67: ["重置时间来自上游快照；未知不代表不重置。时间统一为 Asia/Shanghai。", "Reset times come from upstream snapshots; unknown does not mean no reset. Timezone: Asia/Shanghai.", "重設時間來自上游快照；未知不代表不重設。時區為 Asia/Shanghai。", "リセット時刻は上流のスナップショットに基づきます。不明はリセットなしを意味しません。タイムゾーン：Asia/Shanghai。"],
    ux68: ["账号已归档，保留账号资料、关联与全部历史，停止供给。", "Archived. Account data, links and all history are retained; supply has stopped.", "帳號已封存，保留資料、關聯與全部歷史，停止供給。", "アーカイブ済み。情報、関連付け、全履歴を保持し、供給を停止しています。"]
  });
  Object.assign(words, {
    resourceUX0: ["质量检测", "Quality check", "品質檢測", "品質チェック"],
    resourceUX1: ["新增代理", "Add proxy", "新增代理", "プロキシを追加"],
    resourceUX2: ["删除已选", "Delete selected", "刪除已選", "選択を削除"],
    resourceUX3: ["复制脱敏地址", "Copy safe address", "複製安全位址", "安全なアドレスをコピー"],
    resourceUX4: ["不含用户名和密码", "No username or password", "不含使用者名稱與密碼", "ユーザー名・パスワードなし"],
    resourceUX5: ["脱敏代理地址", "Safe proxy address", "安全代理位址", "安全なプロキシアドレス"],
    resourceUX6: ["点击复制", "Click to copy", "點擊複製", "クリックしてコピー"],
    resourceUX7: ["美国", "United States", "美國", "米国"],
    resourceUX8: ["中国台湾", "Taiwan", "台灣", "台湾"],
    resourceUX9: ["日本", "Japan", "日本", "日本"],
    resourceUX10: ["新加坡", "Singapore", "新加坡", "シンガポール"],
    resourceUX11: ["德国", "Germany", "德國", "ドイツ"],
    resourceUX12: ["加拿大", "Canada", "加拿大", "カナダ"],
    resourceUX13: ["未配置", "Not configured", "未設定", "未設定"],
    resourceUX14: ["洛杉矶", "Los Angeles", "洛杉磯", "ロサンゼルス"],
    resourceUX15: ["台北", "Taipei", "台北", "台北"],
    resourceUX16: ["东京", "Tokyo", "東京", "東京"],
    resourceUX17: ["纽约", "New York", "紐約", "ニューヨーク"],
    resourceUX18: ["法兰克福", "Frankfurt", "法蘭克福", "フランクフルト"],
    resourceUX19: ["多伦多", "Toronto", "多倫多", "トロント"],
    resourceUX20: ["高雄", "Kaohsiung", "高雄", "高雄"],
    resourceUX21: ["大阪", "Osaka", "大阪", "大阪"],
    resourceUX22: ["共 {count} 条 · 每页", "{count} results · Per page", "共 {count} 筆 · 每頁", "{count} 件・1ページ"],
    resourceUX23: ["已选 {count} 个（可跨页） · 当前筛选 {filtered} 个", "{count} selected (across pages) · {filtered} matching filters", "已選 {count} 個（可跨頁） · 目前篩選 {filtered} 個", "{count} 件選択（ページをまたぐ）・検索結果 {filtered} 件"],
    resourceUX24: ["跳过", "Skipped", "略過", "スキップ"],
    resourceUX25: ["等待", "Waiting", "等待", "待機中"],
    resourceUX26: ["检测中", "Testing", "檢測中", "テスト中"],
    resourceUX27: ["执行状态", "Progress", "執行狀態", "実行状態"],
    resourceUX28: ["代理检测结果", "Proxy test results", "代理測試結果", "プロキシテスト結果"],
    resourceUX29: ["手动检测 · 本原型不访问网络", "Manual check · This prototype makes no network requests", "手動測試 · 本原型不連接網路", "手動テスト・このプロトタイプは通信しません"],
    resourceUX30: ["开始演示检测", "Start demo check", "開始演示測試", "デモテストを開始"],
    resourceUX31: ["取消 / 关闭", "Cancel / close", "取消 / 關閉", "キャンセル・閉じる"],
    resourceUX32: ["批量质量检测", "Batch quality check", "批次品質檢測", "一括品質チェック"],
    resourceUX33: ["查看质量报告", "View quality report", "檢視品質報告", "品質レポートを表示"],
    resourceUX34: ["清空选择", "Clear selection", "清除選取", "選択を解除"],
    resourceUX35: ["不含缓存 {count} TPM", "{count} TPM excluding cache", "不含快取 {count} TPM", "キャッシュ除外 {count} TPM"],
    resourceUX36: ["{platform} 配额", "{platform} quota", "{platform} 配額", "{platform} クォータ"],
    resourceUX37: ["含缓存", "Including cache", "含快取", "キャッシュ含む"]
  });
  Object.assign(words, {
    detailUX0: ["账号详情", "Account details", "帳號詳情", "アカウント詳細"],
    detailUX1: ["账号分组", "Account groups", "帳號群組", "アカウントグループ"],
    detailUX2: ["权重", "Weight", "權重", "重み"],
    detailUX3: ["运行指标", "Runtime metrics", "執行指標", "実行指標"],
    detailUX4: ["今日成功率", "Today’s success rate", "今日成功率", "今日の成功率"],
    detailUX5: ["累计成功率", "Lifetime success rate", "累計成功率", "累計成功率"],
    detailUX6: ["编辑账号", "Edit account", "編輯帳號", "アカウント編集"],
    detailUX7: ["OAuth 授权", "OAuth authorization", "OAuth 授權", "OAuth 認証"],
    detailUX8: ["更多操作", "More actions", "更多操作", "その他の操作"],
    detailUX9: ["上次测试（{count} 个）：", "Last test ({count} proxies): ", "上次測試（{count} 個）：", "前回のテスト（{count} 件）："],
    detailUX10: ["5 小时配额已满，预计 {time} 恢复（Asia/Shanghai）。", "5-hour quota exhausted. Expected reset: {time} (Asia/Shanghai).", "5 小時配額已滿，預計 {time} 恢復（Asia/Shanghai）。", "5時間のクォータ上限。リセット予定：{time}（Asia/Shanghai）。"],
    detailUX11: ["当前 5 小时窗口对应模型暂停接收新请求；重登录不会清除限制。", "Models in the current 5-hour window are paused. Reauthorization does not clear the limit.", "目前 5 小時窗口的模型暫停接收新請求；重新登入不會清除限制。", "現在の5時間枠に該当するモデルは受付停止中。再認証で制限は解除されません。"]
  });
  const sourceText=new WeakMap(),sourceAttrs=new WeakMap();
  function translateText(value) {
    const trimmed=value.trim(),index=localeCodes.indexOf(h.locale);
    const phrase=Object.values(words).find(item=>item[0]===trimmed);
    const translated=phrase?.[index]||globalThis.VendorPrototypeTranslations?.[trimmed]?.[index];
    return translated?value.replace(trimmed,translated):value;
  }
  window.vendorPrototypeText=translateText;
  function localizeLegacy() {
    const walker=document.createTreeWalker(document.body,NodeFilter.SHOW_TEXT);
    let node;
    while((node=walker.nextNode())) {
      if(node.parentElement?.closest('script,style,textarea,code,pre'))continue;
      const previous=sourceText.get(node),source=previous&&node.nodeValue===previous.rendered?previous.source:node.nodeValue;
      const rendered=translateText(source);sourceText.set(node,{source,rendered});
      if(node.nodeValue!==rendered)node.nodeValue=rendered;
    }
    for(const element of document.querySelectorAll('[aria-label],[placeholder],[title]')) {
      const previous=sourceAttrs.get(element)||{};
      for(const attribute of ['aria-label','placeholder','title']) {
        if(!element.hasAttribute(attribute))continue;
        const current=element.getAttribute(attribute),saved=previous[attribute],source=saved&&saved.rendered===current?saved.source:current;
        const rendered=translateText(source);previous[attribute]={source,rendered};
        if(current!==rendered)element.setAttribute(attribute,rendered);
      }
      sourceAttrs.set(element,previous);
    }
  }
  const localizationObserver=new MutationObserver(()=>{localizationObserver.disconnect();localizeLegacy();localizationObserver.observe(document.body,{childList:true,subtree:true,characterData:true});});
  localizationObserver.observe(document.body,{childList:true,subtree:true,characterData:true});
  initializeHierarchy();
  setActor('vendor_level1:1');
})();
