这条加进第四节"实现原则"，作为"过度防御"的典型表现，并补自查和示例。

```markdown
写代码时遵守以下规则，目标是"人读得懂、改得快"，不为显得专业而过度设计。避免七种 AI 式偏差：命名大词化、文件内碎拆、注释事无巨细、把 README 塞进代码注释、为兼容旧代码无限妥协适配、为达目的不择手段、过度设计（简单功能加锁/兜底/复杂兼容设计）。

一、函数命名
1. 用"动词 + 业务名词"，动词只用简单词：get / list / add / create / update / delete / remove / check / is / has / to / calc / send / save / load / parse / format / build。
2. 禁止大词和句子式命名：retrieve、persist、execute、acquire、obtain、perform、initialize、validateWhether、processIncoming 等。
3. 函数名不超过 3 个单词；名词用业务词（user/order/product），不用技术抽象词（entity/aggregate/dto/identifier）。
4. 例：listActiveUsers(orgId)、saveUser(user)、checkLogin(username, password)。不要写 retrieveActiveUsersByOrganizationIdentifier、persistUserEntityToDatabase。

二、文件内结构
1. 主流程在一个函数里从上到下顺着写完，不要写成"主流程全是调用 + 底下一堆只调一次的一行小函数"。
2. 只有满足以下之一才抽成独立函数：被调用 2 次以上；一段逻辑超过 15~20 行影响阅读；有独立业务语义且值得命名（如 calcDiscount）；需要单独测试的复杂算法。
3. 只调用一次且只有一两行的逻辑直接内联，不要包装成函数。
4. 不要为"单一职责"把一行代码或一个 if 判断单独抽成函数。单一职责指职责有意义，不是语句只有一行。
5. 不要写纯换名转发的函数（如 addUser 里只有一句 createUser(u)）。有语义的边界封装（如模块对外的 saveUser 包一层 db.insert，便于换实现或加日志）不算此列。

三、修改既有代码：改干净，不为旧代码无限妥协
1. 改某个模块就改干净：该模块内旧逻辑、旧字段、旧分支一并删掉重写，不要保留旧路径再叠加新路径、不要新旧逻辑并存靠 if 兜底。
2. 禁止为兼容历史数据/历史接口/历史调用方，在业务代码里堆适配层、兜底判断、字段映射、"兼容写法"注释。这类数据治理、脏数据修补、版本兼容属于一次性脚本或迁移工具，不进业务代码长期留存。
3. 禁止"防御式妥协"：不确定旧代码是否还有人用就留着、不敢删、加 try/catch 吞掉、加 if 判空掩盖问题。要么确认后删干净，要么在迁移里处理，不要在调用链上层层兜底。
4. 不要保留"死代码 + 注释掉的旧实现 + TODO 以后删"。改就删干净，历史在 git 里。
5. 新逻辑直接按新设计写，不迁就旧的错误抽象。旧抽象阻碍新需求时，改旧抽象本身，而不是在新代码里绕开它、打补丁。
6. 判断标准：这次改动之后，该模块里还存在"为了兼容而存在、新需求根本不需要"的代码吗？有就是没改干净。

四、实现原则：简单优先，目的达不到就说，不为达成目的不择手段
1. 简单功能就顺着写：一个简单流程就一行一行按顺序写完，不要为了"健壮/通用/可扩展"引入锁、队列、状态机、重试框架、复杂兜底设计。大多数兜底场景一辈子不会发生。
2. 过度设计的具体禁止项：给单线程/单请求场景加锁；给基本不会变的字段做动态兼容；给不可能并发的地方加并发控制；给正常返回加多层 try/catch 兜底；为"以后可能扩展"提前抽象接口/工厂/策略；为"字段可能变"给外部接口返回做映射层。这些在 AI 手里最常出现。
3. 兜底要一个一个来，不要建通用兜底框架：真需要兜底时，在具体那处写一个明确的 if/默认值即可，不要为了兜底造一套"容错层/兼容层/降级框架"。兜底的成本要小于它防的事。
4. 外部接口返回的字段默认稳定，不要防御式映射：别的接口返回的结构基本不会变，直接按约定用它的字段，不要逐字段判空、改名、给默认值、做映射。真变了是接口契约问题，去改契约或升级，不要在调用方堆兼容。
5. 动手前先判断架构是否可行：该需求在当前架构下能不能干净地实现？如果必须靠 hack、靠魔改框架、靠绕过现有分层、靠全局变量/反射/临时状态才能做到，说明架构不支撑这个需求，应停下来说明，而不是硬做。
6. 分层职责不能为"达成目的"让路：差异化逻辑必须落在它该在的层（适配层/厂商层/领域层），不能为了方便把本该下沉的逻辑硬编码进上层（尤其是 service/controller）。靠上层 if 分支把不同厂商/不同来源的差异"抹平"，是假统一，不是统一。
7. 做不到就说：统一对接某类厂商/接口时，如果某个厂商在语义上或接口能力上根本对不齐，应明确告知"这个厂商在当前抽象下接不了/只能降级支持"，给出替代方案或要求调整抽象，而不是在上层硬编码特例、假装统一成功。
8. 禁止"能跑就行"的肮脏实现：为让功能跑通而塞 hack、打补丁、绕过类型检查、吞异常、写死临时值、改公共底层迁就单个需求。
9. 不要在错误的架构上继续堆错误：如果实现过程中发现前提不成立（数据模型不对、分层放错、抽象设计有缺陷），先停下来指出，不要为了把当前需求做完而继续在错误基础上加代码。
10. 区分"暂时妥协"和"肮脏实现"：可接受的临时方案必须显式标注（TODO + 原因 + 何时还），且不扩散到其他模块；不可接受的肮脏实现一律不做。
11. 判断标准：这个实现换个人维护会不会骂街？这个"健壮"防的事是不是基本不会发生？这个"统一"是不是靠上层 if 硬抹平的假统一？这个抽象是不是为"以后可能"提前造的？有一个是，就是过度设计或没写好。

五、注释
1. 只写代码本身说不出来的"为什么"：设计决策、业务规则、坑、临时 workaround、TODO。不写代码在干什么。
2. 简短：一句话能说清就一句话。不写翻译式注释。不写文档式注释块把参数返回值异常全列一遍，除非公共 API 且团队要求。
3. 不写流程、不列清单、不写实现步骤——代码一改注释必漂移，这类内容放 README。
4. 不把 README 塞进文件头：产品定位、部署形态、配置项清单、数据目录清单、分层说明一律去 README。文件头注释不超过 2 行，只写"这个文件是什么、为什么存在"。
5. 注释里出现第二个文件名就要警惕——说明在写跨文件文档，不是注释，应搬去 README 或设计文档。
6. 超过 3 行的块注释先问能不能搬去 README。一段代码必须靠长注释才看懂，先改代码。

六、自查（写完代码后检查）
1. 读主流程需要往下跳几次？超过 2 次就合并回来。
2. 有没有只调一次且 ≤3 行的函数？有就内联。
3. 有没有函数名比实现还长？有就内联或改名。
4. 函数名里有没有大词或句子式命名？有就改成简单动词 + 业务名词。
5. 改完之后，模块里还有没有"只为兼容旧逻辑而存在、新需求不需要"的代码、兜底 if、适配映射？有就删干净或移去迁移脚本。
6. 这个简单流程，我是不是加了锁/队列/状态机/重试/多层兜底/提前抽象？加的这些防的事是不是基本不会发生？是就去掉，顺着写。
7. 兜底是不是造了一套通用框架？是就改成在具体处一个一个兜底。
8. 外部接口返回的字段，我是不是在做逐字段判空/改名/默认值映射？是就直接按约定用，契约问题去改契约。
9. 这个实现有没有靠 hack、绕过分层、全局状态、吞异常、写死临时值才跑通？有就说明架构不支撑，停下来指出。
10. 有没有把本该下沉的差异化逻辑（厂商差异、来源差异）硬编码在上层 service/controller？有就是假统一，下沉到适配层或告知做不到。
11. 实现过程中有没有发现前提不成立却继续往下写？有就先说。
12. 注释是不是在复述代码？是就删。注释是不是超过 3 行？先考虑搬 README 或砍成一句"为什么"。
13. 代码改了，旁边注释还对不对？不对就改注释或删注释。

七、示例

【命名】
错误：retrieveActiveUsersByOrganizationIdentifier(orgId)、persistUserEntityToDatabase(userEntity)、executeValidationOfUserCredentials(u, p)
正确：listActiveUsers(orgId)、saveUser(user)、checkLogin(username, password)

【文件内结构】
错误（主流程全是调用，底下一堆一次性小函数）：
export function createUser(input) {
  validateInput(input);
  const cleaned = cleanInput(input);
  const user = buildUser(cleaned);
  const saved = saveUser(user);
  return formatUser(saved);
}
// 底下 9 个函数各 1~4 行，全只调一次

正确（主流程顺着写完，值得抽才抽）：
export function createUser(input) {
  if (!input?.email) throw new Error('no email');
  if (!/^\S+@\S+$/.test(input.email)) throw new Error('bad email');
  const user = {
    id: Date.now().toString(36),
    name: (input.name || '').trim(),
    email: input.email.trim(),
    createdAt: new Date(),
  };
  const saved = db.users.insert(user);
  return { ...saved, name: saved.name || '匿名' };
}

【什么才值得抽】
错误：calcBase/calcOff/roundPrice 三个一行函数，各只调一次
正确：一行能写清就内联；逻辑复杂、有独立语义、值得单测才抽（如 calcOrderTotal 含阶梯折扣+满减）

【纯换名转发 vs 边界封装】
错误：function addUser(u) { return createUser(u); }
正确：export function saveUser(user) { return db.users.insert(user); } // 模块对外接口，换 DB/加日志在这改

【改干净，不为旧代码妥协】
错误（新旧逻辑并存、无限兜底适配）：
export function getUser(id) {
  const row = db.users.get(id);
  if (!row) return null;
  const name = row.name ?? row.userName ?? '';
  const email = row.email || row.mail || '';
  const status = row.status === 1 ? 'active' : row.status === 0 ? 'inactive' : row.status;
  return { id: row.id, name, email, status };
}
正确（业务代码只按新结构写，脏数据交给迁移脚本）：
export function getUser(id) {
  const row = db.users.get(id);
  if (!row) return null;
  return { id: row.id, name: row.name, email: row.email, status: row.status };
}
// 旧字段 userName/mail/数字 status 由一次性迁移脚本 users_migrate_v2 处理，不在业务代码里兜底

【过度设计：简单功能加锁/兜底 vs 顺着写】
错误（一个简单的"读配置并缓存"加了锁、双检、多层兜底、兼容映射）：
let cache = null;
let lock = new Mutex();
export async function getConfig() {
  await lock.acquire();
  try {
    if (cache) return cache;
    let raw;
    try {
      raw = await fs.readFile('config.json', 'utf8');
    } catch (e) {
      raw = '{}'; // 兜底空配置
    }
    let parsed;
    try {
      parsed = JSON.parse(raw);
    } catch (e) {
      parsed = {}; // 兜底解析失败
    }
    // 担心字段会变，逐字段兼容
    const cfg = {
      port: parsed.port ?? parsed.Port ?? 3000,
      host: parsed.host ?? parsed.Host ?? '0.0.0.0',
      db: parsed.db ?? parsed.database ?? {},
    };
    cache = cfg;
    return cache;
  } finally {
    lock.release();
  }
}
// 问题：单进程启动读一次配置，没有并发，锁是多余的；解析失败该直接报错，
// 吞成 {} 会让配置错误静默；字段兼容是防御基本不会发生的事，把代码弄难懂。

正确（顺着写，该报错就报错）：
export async function getConfig() {
  if (cache) return cache;
  const raw = await fs.readFile('config.json', 'utf8'); // 读不到就报错，正常暴露
  cache = JSON.parse(raw);                              // 解析失败就报错
  return cache;
}

【外部接口字段：不要防御式映射】
错误（担心别的接口返回结构变，逐字段判空+改名+默认值）：
export async function getWeather(city) {
  const res = await http.get(`/api/weather?city=${city}`);
  const data = res.data ?? {};
  const temp = data.temp ?? data.temperature ?? data.tempC ?? 0;
  const hum = data.humidity ?? data.hum ?? 0;
  const wind = data.wind ?? data.windSpeed ?? 0;
  return { temp, hum, wind };
}
正确（接口契约约定字段稳定，直接用）：
export async function getWeather(city) {
  const res = await http.get(`/api/weather?city=${city}`);
  const { temp, humidity, wind } = res.data;
  return { temp, hum: humidity, wind };
}
// 字段真变了是接口契约问题，去改契约或升级接口版本，不在调用方堆兼容。

【假统一：上层硬编码抹平厂商差异 vs 下沉到厂商层 / 说明做不到】
需求：统一对接多个短信厂商，service 提供 sendSms(phone, content)。
错误（service 层硬编码厂商差异）：
export function sendSms(vendor, phone, content) {
  if (vendor === 'aliyun') { /* ... */ }
  else if (vendor === 'tencent') { /* ... */ }
  else if (vendor === 'twilio') { /* twilio 无模板 */ }
  else if (vendor === 'yunpian') { /* 需签名 */ }
  throw new Error('unknown vendor');
}
正确（差异下沉到厂商适配层；对不齐的显式说明）：
// vendor/sms/aliyun.js、twilio.js…各实现 sendSms，自己处理字段映射/签名/模板差异
// service/smsService.js 只调统一接口，不含厂商分支
export function sendSms(vendor, phone, content) {
  const impl = vendors[vendor];
  if (!impl) throw new Error(`unknown vendor: ${vendor}`);
  return impl.sendSms({ phone, content });
}
// 并在契约里明确：twilio/云片不支持模板；需模板能力的场景标记"不支持"，由调用方决定降级或拒绝。

【为达目的不择手段（魔改第三方）vs 先判断架构可行性】
错误：
export function markRead(msg) {
  readState.set(msg.id, true);
  msg.__read = true;
  Object.getPrototypeOf(msg).isRead = () => readState.get(msg.id);
}
正确：
// 第三方消息对象只读，"已读"是本地状态，独立建模，不魔改第三方对象。
export function markRead(id) { readStore.set(id, true); }
// 若要求"已读"跟随消息对象走，需确认第三方扩展机制；当前架构做不到干净实现，不硬做。

【注释】
错误（事无巨细、复述代码、README 塞文件头）：
/**
 * 计算订单总价
 * 遍历订单项，把每一项的价格乘以数量累加到 total
 * 然后判断 total 是否大于 1000，如果大于就打 9 折
 * ...
 */
export function calcOrderTotal(items) { ... }

正确（只写为什么，简短）：
export function calcOrderTotal(items) {
  // 折扣规则来自产品 2024-03 需求，满减与折扣可叠加
  let total = items.reduce((s, it) => s + it.price * it.qty, 0);
  if (total > 1000) total *= 0.9;
  else if (total > 500) total *= 0.95;
  if (total > 2000) total -= 100;
  return Math.round(total * 100) / 100;
}
// core 进程入口，只做装配；形态、配置与分层见 README。
// 重启采用换代模型：旧 server 优雅关闭后重建，boot 计数供前端确认新实例就绪。

八、一句话总结
AI 爱写"俄罗斯套娃"、"注释说明书"、"无限兼容的补丁层"、"能跑就行的 hack"、"上层 if 硬抹平的假统一"和"简单功能加锁加兜底的过度设计"，人该写"一条直线"、"一句为什么"、"改干净的模块"、"架构不支撑就先说"、"差异下沉到适配层"和"大多数兜底用不到就别加"。抽函数的理由只有四个：复用、太长、语义独立、要单测。写注释的理由只有一个：代码本身说不出的"为什么"。改代码的原则只有一个：该模块按新设计改干净，历史兼容交给迁移脚本。实现的原则只有一个：能顺着写就顺着写，别为基本不会发生的事加锁加兜底加抽象；架构支撑就干净地做，不支撑就停下来说清楚。判断标准：读主流程要不要往下跳？注释是不是在翻译代码？改完还有没有"只为兼容而存在"的代码？这个实现是不是靠 hack 硬凑？这个"统一"是不是靠上层 if 硬抹平的？这个锁/兜底/抽象防的事是不是基本不会发生？有一个是，就是没写好。
```