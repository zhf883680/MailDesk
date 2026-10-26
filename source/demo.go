package main

import (
	"fmt"
	"time"
)

// demoMailboxes / seedDemo 提供一组演示数据，方便在没有真实邮箱时预览界面
func seedDemo(cm *ConfigManager, st *Store) {
	if len(cm.Get().Mailboxes) > 0 {
		return
	}
	_ = cm.Update(func(c *Config) error {
		c.Demo = true
		c.AI.Enabled = true
		c.AI.Model = "gpt-4o-mini"
		c.AI.BaseURL = "https://api.openai.com/v1"
		c.AI.Translate = true
		c.AI.Classify = true
		c.Mailboxes = []Mailbox{
			{ID: "d1work", Name: "工作邮箱", Email: "alex@northwind.studio", Color: "#0d6b5f", Demo: true,
				IMAPHost: "imap.northwind.studio", IMAPPort: 993, IMAPTLS: true, SMTPHost: "smtp.northwind.studio", SMTPPort: 465, SMTPTLS: true,
				LastSync: time.Now().Add(-3 * time.Minute)},
			{ID: "d2me", Name: "个人邮箱", Email: "alex.chen@gmail.com", Color: "#b7791f", Demo: true,
				IMAPHost: "imap.gmail.com", IMAPPort: 993, IMAPTLS: true, SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPTLS: true,
				LastSync: time.Now().Add(-3 * time.Minute)},
			{ID: "d3news", Name: "订阅与通知", Email: "hello@designweekly.io", Color: "#5b5bd6", Demo: true,
				IMAPHost: "imap.designweekly.io", IMAPPort: 993, IMAPTLS: true, SMTPHost: "smtp.designweekly.io", SMTPPort: 587, SMTPTLS: false,
				LastSync: time.Now().Add(-3 * time.Minute)},
		}
		c.Forward = ForwardConfig{
			Enabled:   true,
			BaseURL:   "https://api.day.app",
			DeviceKey: "demoDeviceKey",
			Group:     "MailDesk",
			Level:     "timeSensitive",
		}
		c.ForwardRules = []ForwardRule{{
			ID: "fr1", Name: "验证码即时转发", Enabled: true, Color: "#b0761c",
			Keywords:      []string{"验证码", "verification code", "security code", "passcode"},
			TitleTemplate: "🔐 {{subject}}",
			BodyTemplate:  "{{code}}\n来自 {{from}} · {{mailbox}}",
			MailboxIDs:    []string{},
		}, {
			ID: "fr2", Name: "客户来信提醒", Enabled: false, Color: "#0f6b5c",
			Keywords:   []string{"报价", "quote", "合同", "contract"},
			MailboxIDs: []string{"d1work"},
		}}
		c.Rules = []Rule{{
			ID: "r1", Name: "客户询价提取", Enabled: true, Color: "#0d6b5f",
			Keywords: []string{"quote", "pricing", "budget"},
			Prompt:   "这是一封客户询价邮件。请提取：1) 客户公司 2) 需求内容 3) 预算区间 4) 期望交付时间。用简短条目列出，若邮件中未提及请写“未提及”。",
		}, {
			ID: "r2", Name: "账单信息提取", Enabled: true, Color: "#b7791f",
			Keywords: []string{"invoice", "billing", "receipt"},
			Prompt:   "请从账单邮件中提取：金额（含币种）、到期日、账单周期、付款方式。用 4 个条目输出。",
		}}
		return nil
	})
	mbs := cm.Get().Mailboxes
	now := time.Now()
	add := func(e *Email) {
		e.AIStatus = "done"
		e.AIAt = now
		e.ID = makeID(e.MailboxID, e.UID)
		st.Upsert(e)
	}
	// —— 工作邮箱 ——
	add(&Email{
		MailboxID: mbs[0].ID, UID: 101, Subject: "Re: Q4 Campaign — final scope & timeline",
		FromName: "Marta Lindqvist", FromAddr: "marta@brightside.co",
		To:   []Addr{{Name: "Alex Chen", Addr: "alex@northwind.studio"}},
		Date: now.Add(-42 * time.Minute), Seen: true, Flagged: true,
		SubjectZh: "回复：第四季度推广 — 最终范围与时间表",
		Snippet:   "Hi Alex, thanks for the revised deck. Our leadership is aligned on the scope, but we need the launch...",
		HTML: `<div style="font-family:-apple-system,Segoe UI,sans-serif;color:#222">
<h2 style="color:#0f6b5c">Q4 Campaign — Scope &amp; Timeline</h2>
<p>Hi Alex, thanks for the revised deck. Our leadership is aligned on the scope, but we need the launch moved to <strong>November 18</strong> so the retail team can prepare the in-store assets.</p>
<table border="1" cellpadding="8" cellspacing="0" style="border-collapse:collapse;width:100%">
<tr style="background:#f5f1e9"><th align="left">Item</th><th align="right">Budget (USD)</th></tr>
<tr><td>Localized landing pages (DE / FR)</td><td align="right">12,400</td></tr>
<tr><td>Paid social creative</td><td align="right">6,800</td></tr>
</table>
<blockquote>Contract signing target: Friday.</blockquote>
</div>`,
		Text: `Hi Alex,

Thanks for the revised deck — our leadership is aligned on the scope, but we need the launch moved to November 18 so the retail team can prepare the in-store assets.

Two questions before we sign:
1. Can you include the localized landing pages for DE and FR in the same budget?
2. Who owns the paid social creative — us or your team?

If both are fine, we can have the contract signed by Friday.

Best,
Marta
Head of Brand, Brightside`,
		Translated: `Alex 你好，

感谢你修改后的方案——我们管理层对项目范围已达成一致，但需要把上线时间推迟到 11 月 18 日，以便零售团队准备门店物料。

签约前有两个问题：
1. 能否在同样的预算内包含德语和法语的本地化落地页？
2. 付费社交素材由谁负责——我们还是你们团队？

如果这两点都没问题，我们可以在周五前完成合同签署。

祝好，
Marta
Brightside 品牌负责人`,
		Summary:    "客户同意方案，要求上线推迟至 11 月 18 日，并询问本地化页面与素材归属，需周五前答复。",
		Importance: "high", NeedAction: true, Category: "工作",
		RuleResults: []RuleResult{{RuleID: "r1", RuleName: "客户询价提取", Color: "#0d6b5f", At: now, Content: `**客户公司**：Brightside（零售品牌）
**需求内容**：Q4 推广项目，含 DE / FR 本地化落地页与付费社交素材
**预算区间**：沿用修订版报价（未提及新增预算）
**期望交付时间**：11 月 18 日上线，本周五前签约`}},
	})
	add(&Email{
		MailboxID: mbs[0].ID, UID: 102, Subject: "Design review notes — dashboard v3",
		FromName: "Priya Raman", FromAddr: "priya@northwind.studio",
		To:   []Addr{{Name: "Design Team", Addr: "design@northwind.studio"}},
		Date: now.Add(-3 * time.Hour), Seen: false,
		SubjectZh: "设计评审记录 — 仪表盘 v3",
		Snippet:   "Sharing the notes from today's review. Three blockers before we hand off to engineering...",
		Text: `Team,

Sharing the notes from today's review. Three blockers before we hand off to engineering:

- The empty state for the activity feed still has no illustration.
- Chart colors fail contrast on the dark surface (2.9:1).
- We need a decision on whether filters persist across sessions.

I've left comments in Figma. Please resolve by Thursday EOD.

Priya`,
		Translated: `各位，

附上今天评审的记录。交付给工程团队之前还有三个阻塞项：

- 动态列表的空状态仍缺少插图。
- 深色背景下的图表配色对比度不足（2.9:1）。
- 需要决定筛选条件是否跨会话保留。

我已在 Figma 里留言，请在周四下班前处理完。

Priya`,
		Summary:    "设计评审提出 3 个阻塞项（空状态插图、图表对比度、筛选记忆），需周四前解决。",
		Importance: "medium", NeedAction: true, Category: "工作",
	})
	add(&Email{
		MailboxID: mbs[0].ID, UID: 103, Subject: "Your invoice #INV-2084 from Cloudbase",
		FromName: "Cloudbase Billing", FromAddr: "billing@cloudbase.io",
		To:   []Addr{{Name: "Alex Chen", Addr: "alex@northwind.studio"}},
		Date: now.Add(-7 * time.Hour), Seen: true,
		SubjectZh: "Cloudbase 发票 #INV-2084",
		Snippet:   "Your monthly invoice is ready. Amount due: $248.00, payment will be charged on Oct 28...",
		Text: `Hello Alex,

Your monthly invoice is ready.

Invoice number: INV-2084
Billing period: Oct 1 – Oct 31, 2026
Amount due: $248.00 USD
Payment method: Visa ending 4242
Auto-charge date: Oct 28, 2026

You can download the PDF from your dashboard.

— Cloudbase Billing`,
		Translated: `Alex 你好，

你的月度账单已生成。

发票编号：INV-2084
账期：2026 年 10 月 1 日 – 10 月 31 日
应付金额：248.00 美元
付款方式：尾号 4242 的 Visa 卡
自动扣款日期：2026 年 10 月 28 日

可在控制台下载 PDF 版本。

— Cloudbase 账单团队`,
		Summary:    "云服务月账单 248 美元，将于 10 月 28 日自动扣款，无需操作。",
		Importance: "low", NeedAction: false, Category: "账单",
		RuleResults: []RuleResult{{RuleID: "r2", RuleName: "账单信息提取", Color: "#b7791f", At: now, Content: `**金额**：$248.00 USD
**到期日**：2026-10-28（自动扣款）
**账单周期**：2026-10-01 至 2026-10-31
**付款方式**：Visa 尾号 4242`}},
	})
	add(&Email{
		MailboxID: mbs[0].ID, UID: 104, Subject: "Intro: Sarah from Hatch Labs",
		FromName: "Daniel Okoye", FromAddr: "daniel@hatchlabs.com",
		To:   []Addr{{Name: "Alex Chen", Addr: "alex@northwind.studio"}},
		Date: now.Add(-26 * time.Hour), Seen: true,
		SubjectZh: "引荐：Hatch Labs 的 Sarah",
		Snippet:   "Quick intro — Sarah leads product at Hatch Labs, they're looking for a design partner for...",
		Text: `Hi Alex,

Quick intro — Sarah leads product at Hatch Labs, they're looking for a design partner for a new fintech product in Q1.

Sarah, Alex runs Northwind Studio, they did the onboarding revamp for Brightside.

Happy to take it offline if you two want to talk.

Daniel`,
		Translated: `Alex 你好，

简单引荐一下——Sarah 在 Hatch Labs 负责产品，他们正在为明年一季度的一款金融科技新产品寻找设计合作方。

Sarah，Alex 经营 Northwind Studio，Brightside 的入驻流程改版就是他们做的。

如果你们想聊，我就不打扰了。

Daniel`,
		Summary:    "商务引荐：Hatch Labs 产品负责人寻找 Q1 金融产品设计合作方。",
		Importance: "medium", NeedAction: true, Category: "工作",
	})
	// —— 个人邮箱 ——
	add(&Email{
		MailboxID: mbs[1].ID, UID: 201, Subject: "Action required: confirm your GitHub SSH key",
		FromName: "GitHub", FromAddr: "noreply@github.com",
		To:   []Addr{{Name: "alex.chen", Addr: "alex.chen@gmail.com"}},
		Date: now.Add(-2 * time.Hour), Seen: false,
		SubjectZh: "需要操作：确认你的 GitHub SSH 密钥",
		Snippet:   "We noticed a new SSH key was added to your account on Oct 23 at 09:14 UTC...",
		Text: `Hi alexchen,

We noticed a new SSH key was added to your account on Oct 23, 2026 at 09:14 (UTC).

Key fingerprint: SHA256:9xQ2...kV8
If this was you, no action is needed. If you don't recognize this key, please review your security settings immediately.

Thanks,
The GitHub Team`,
		Translated: `alexchen 你好，

我们注意到 2026 年 10 月 23 日 09:14（UTC），你的账户新增了一个 SSH 密钥。

密钥指纹：SHA256:9xQ2...kV8
如果是你本人操作，无需处理。若你不认识这个密钥，请立即检查安全设置。

谢谢，
GitHub 团队`,
		Summary:    "GitHub 账户新增 SSH 密钥通知，非本人操作需立即检查安全设置。",
		Importance: "high", NeedAction: true, Category: "通知",
	})
	add(&Email{
		MailboxID: mbs[1].ID, UID: 202, Subject: "你的 ChatGPT 月度订阅已续费",
		FromName: "OpenAI", FromAddr: "noreply@openai.com",
		To:   []Addr{{Name: "alex.chen", Addr: "alex.chen@gmail.com"}},
		Date: now.Add(-8 * time.Hour), Seen: true,
		Snippet: "你的 Plus 订阅已成功续费 20.00 美元，扣款方式为 Visa 尾号 4242。",
		Text: `你好，

你的 ChatGPT Plus 订阅已成功续费。

金额：20.00 美元
周期：2026 年 10 月 23 日 – 2026 年 11 月 22 日
付款方式：Visa 尾号 4242

如非本人操作，请回复此邮件。

OpenAI 团队`,
		Summary:    "ChatGPT Plus 续费 20 美元成功，属于常规扣费。",
		Importance: "low", NeedAction: false, Category: "账单",
	})
	add(&Email{
		MailboxID: mbs[1].ID, UID: 203, Subject: "双 11 预售开启：全场 5 折起，仅限今日",
		FromName: "优选好物", FromAddr: "promo@youxuan-mall.com",
		To:   []Addr{{Name: "alex.chen", Addr: "alex.chen@gmail.com"}},
		Date: now.Add(-5 * time.Hour), Seen: false,
		Snippet: "年度最低价！数码、家居、美妆全场 5 折起，前 1000 名下单再减 100 元……",
		Text: `年度最低价！数码、家居、美妆全场 5 折起，前 1000 名下单再减 100 元。

点击查看专场 >>
退订请点此。`,
		Summary:    "电商双 11 促销广告，可直接忽略。",
		Importance: "low", NeedAction: false, IsAd: true, Category: "营销",
	})
	add(&Email{
		MailboxID: mbs[1].ID, UID: 204, Subject: "Security alert: new sign-in from Singapore",
		FromName: "Notion", FromAddr: "security@notion.so",
		To:   []Addr{{Name: "alex.chen", Addr: "alex.chen@gmail.com"}},
		Date: now.Add(-30 * time.Hour), Seen: true,
		SubjectZh: "安全提醒：来自新加坡的新登录",
		Snippet:   "A new device signed in to your Notion account from Singapore on Oct 22 at 23:41...",
		Text: `Hi Alex,

A new device signed in to your Notion account:

Location: Singapore, SG
Time: Oct 22, 2026, 23:41 (GMT+8)
Device: Chrome on macOS

If this wasn't you, secure your account now.

Notion Security`,
		Translated: `Alex 你好，

有新设备登录了你的 Notion 账户：

位置：新加坡
时间：2026 年 10 月 22 日 23:41（GMT+8）
设备：macOS 上的 Chrome

如果这不是你本人操作，请立即保护账户安全。

Notion 安全团队`,
		Summary:    "Notion 出现来自新加坡的异地登录，若不是本人需立即改密。",
		Importance: "high", NeedAction: true, Category: "通知",
	})
	add(&Email{
		MailboxID: mbs[1].ID, UID: 205, Subject: "Your Stripe verification code",
		FromName: "Stripe", FromAddr: "no-reply@stripe.com",
		To:   []Addr{{Name: "alex.chen", Addr: "alex.chen@gmail.com"}},
		Date: now.Add(-18 * time.Minute), Seen: false,
		SubjectZh: "Stripe 验证码",
		Snippet:   "Your verification code is 482913. It expires in 10 minutes...",
		Text: `Hi Alex,

Your verification code is 482913.

This code expires in 10 minutes. If you didn't request it, you can safely ignore this email.

— Stripe`,
		Translated: `Alex 你好，

你的验证码是 482913。

该验证码将在 10 分钟后失效。如果并非你本人操作，可忽略此邮件。

— Stripe`,
		Summary:    "Stripe 登录验证码 482913，10 分钟内有效。",
		Importance: "high", NeedAction: true, Category: "验证码",
		Forwarded:  true,
		ForwardLog: []ForwardLog{{RuleName: "验证码即时转发", OK: true, At: now.Add(-18 * time.Minute)}},
	})
	// —— 订阅邮箱 ——
	add(&Email{
		MailboxID: mbs[2].ID, UID: 301, Subject: "Design Weekly #218 — The quiet comeback of skeuomorphism",
		FromName: "Design Weekly", FromAddr: "hello@designweekly.io",
		To:   []Addr{{Name: "Alex", Addr: "hello@designweekly.io"}},
		Date: now.Add(-6 * time.Hour), Seen: false,
		SubjectZh: "设计周刊 #218 — 拟物化设计的悄然回归",
		Snippet:   "This week: why texture is back, a teardown of Linear's new settings page, and 12 free...",
		Text: `This week in design:

1. The quiet comeback of skeuomorphism — why texture is returning to interfaces.
2. Teardown: Linear's new settings page.
3. 12 free illustration packs for empty states.
4. Reader question: how do you document design decisions?

Read online →
Unsubscribe`,
		Translated: `本周设计精选：

1. 拟物化的悄然回归——为什么质感正在重回界面。
2. 拆解：Linear 全新的设置页面。
3. 12 套免费空状态插图资源。
4. 读者提问：你如何记录设计决策？

在线阅读 →
退订`,
		Summary:    "设计周刊第 218 期，主题为拟物化回归与 Linear 设置页拆解，可稍后阅读。",
		Importance: "low", NeedAction: false, Category: "营销",
	})
	add(&Email{
		MailboxID: mbs[2].ID, UID: 302, Subject: "Your weekly analytics digest",
		FromName: "Plausible", FromAddr: "reports@plausible.io",
		To:   []Addr{{Name: "Alex", Addr: "hello@designweekly.io"}},
		Date: now.Add(-20 * time.Hour), Seen: true,
		SubjectZh: "你的每周分析报告",
		Snippet:   "Last week your site had 12,480 unique visitors (+18%), 3.2 pages per visit...",
		Text: `Last week your site had:

- 12,480 unique visitors (+18%)
- 3.2 pages per visit
- 62% bounce rate
- Top source: Hacker News (4,102 visits)

View full report →`,
		Translated: `上周你的网站数据：

- 独立访客 12,480（+18%）
- 人均浏览 3.2 页
- 跳出率 62%
- 主要来源：Hacker News（4,102 次访问）

查看完整报告 →`,
		Summary:    "网站周报：访客 12,480（+18%），跳出率 62%，Hacker News 带来主要流量。",
		Importance: "medium", NeedAction: false, Category: "通知",
	})
	add(&Email{
		MailboxID: mbs[2].ID, UID: 303, Subject: "【重要】域名 northwind.studio 将于 15 天后到期",
		FromName: "阿里云", FromAddr: "noreply@aliyun.com",
		To:   []Addr{{Name: "Alex", Addr: "hello@designweekly.io"}},
		Date: now.Add(-2 * 24 * time.Hour), Seen: true,
		Snippet: "你的域名 northwind.studio 将于 2026 年 11 月 7 日到期，请及时续费以免影响网站访问。",
		Text: `尊敬的用户：

你的域名 northwind.studio 将于 2026 年 11 月 7 日到期。
为避免域名被释放，请于到期前完成续费。续费价格：¥ 89/年。

立即续费 >>
阿里云`,
		Summary:    "域名 northwind.studio 15 天后到期，需尽快续费 ¥89/年。",
		Importance: "high", NeedAction: true, Category: "账单",
	})
	fmt.Println("演示数据已载入")
}
