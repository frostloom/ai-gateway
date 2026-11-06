# 原3660对评测：V14技术准备及一次整窗政策变更

状态：技术验证及最终独审已完成，可提交一次具体600秒整窗政策批准。尚未批准600秒政策、签发实际执行授权或启动新owner；评测目标未完成。

## 固定范围

原1220题×3＝3660对、7320臂、11772定义轮、1851边界不缩减。原55窗385对保留主样本，新的完整3275对为6550臂、10504轮、456本地窗，对应原056—511窗；原顺序、caps、两臂knowledge=off均保持。旧V13正常174对保留补充记录，不冒充新3275对完成证明；停止21窗只audit/评分hold。所有旧scope永久closed，不恢复、不重领、不释放177份永久预留。

候选只将新V14整窗上限300→600秒，计时仍包含每窗历史验证，保留context取消与全部检查。单次模型90秒、JEV10秒和旧1—13代300秒原记录均不变。已批准的严格早期TLS纯EOF、同context同body一次native2条件保持，每提供者全范围最多20次extra；其他失败立即停止，不自动重启/续跑。

| 固定材料 | 精确身份 |
|---|---|
| 源码commit | aa84bb5e4cb56ae847f6a1649aa60ef1a77d21f3 |
| 源码tree | 077b52e02e4b00e9fe4b8165a626854ad2ee92b0 |
| 2547文件/626Go source map | 272f88edded55dbc35964a1284e73310f31187d486c079fb398015703dfb423f |
| manifest | 1d1c16ceda735dbd11376c4f6f5383a9344f5c0c45327c051d09e5f0a6cf1416 |
| binary | 4f4bba29a2ce7a08016b2670e7c9e2ddc8caf793cbcfb26647c1277c60fe174f |
| scope | 411830efff07a1932a1683ea9f071603c57c9827f2927699d734d6de60f69d51 |
| execution | fbc5a790b168d99acd4d2f312b29179078e2e6ef21fd49ba88db72a6612fbb93 |
| 600/90/10政策 | 40f1ad8fe36d38a54e43e4d7fdc319d1137c8cde6aeac2255a200a44370f83a3 |
| pre-authority readiness | c6a3a38db725ef73ac4b5eafb723b3a504a14f6da43ddf87eb41473f6b1abdc7 |

实际路径为`.tmp/paired-live-private/full3660-completion-v14-freeze-20261106-r1`。私密配置及签名私钥不进入报告；技术签名不能代替实际政策或执行许可。source `.git`指向独立source-git；全Go之后只收紧文件到0400/0500，字节、Git内容及执行位不变。旧未采用技术材料和各失败回执完整保留。

## 已运行的验证

精确归档全Go：4665项通过、0失败、8项既有条件跳过，23包通过、26包无测试，1859.734秒、exit0、reaped/groupempty；归档源码和Go工具未变，原9服务同ID/image/config/mount恢复健康。首次因错误Python3.9子进程失败的回执保留；新独立R2固定Python3.12完成，未把首次失败改标通过。

真正177停止历史构造114.740秒通过，旧156完整proof和132145物理读取multiset保持。normal/race各62项通过，Python新组件99项通过；受控替身双窗9对18臂44轮及后续确认通过，0真实provider/金融claim。

当前真实构建binary的只读`--prepare-v14`通过，返回3275对/6550臂/10504轮，明确read_only=true、actual_execution_authorized=false、operation_policy_approval_required=true。3个预构建source/capture/spool助手及单独交付助手已固定；新binary的source/spool2项和本地HTTP receiver交付2项通过，0外部Langfuse IO。私密配置经实际Go reader/digest/spool/脱敏observer identity验证，未构造Observer或发起DB/网络连接。

原385对前缀已在选定新verifier下重新验证：55次真实Go source replay＋55次内容capture，共110助手、1069.627秒，全部exit0/accepted/reaped/groupempty。原770臂1268轮、2300评分、6849观测及10个原正分抑制孔保持，原saved GET、once/ACK、内容/父链/业务工具/最终回答均核对；0新POST/GET。报告SHA893fbb7d04828b73ea2a74b813661d21f497d967b66efdfccc8342b71629aa4b。supervision另保留3个先前成功build回执，总113；前缀调用数仅source55＋capture55，不混入build。

独立源码/原件等价审查881e994b91c49052a025f21abdbffd72edf4028127fa160e64fa9c4dfae3377b；最终渲染/4技术签名/10绑定材料/driver/DTO/预算审查cd7bb41120741307563826f773b0761376c07f8215ed339c3f2efe10df3944bc，各限定范围0未关闭P1/P2。bootstrap技术审计原字节保留，readiness addendum补齐前缀及最终检查，不把bootstrap改称人类批准。

## 预算和执行边界

全局上限仍¥200＋$4，新Parent为¥200＋$3。177份永久预留为¥85.037976＋$0.932373，不释放。456窗原base合¥110.191257＋$1.818872，连风险、最大40次extra和历史的最大责任¥198.747697＋$2.805070，Parent余量¥1.252303＋$0.194930；预留不是供应商账单。

最多52520次逻辑模型/5252次逻辑JEV，含各20 extra的native上限52540/5272。固定DeepSeek deepseek-flash及TypeSafe jev-1.13.0，发送合成租户对话/工具上下文与合成评判状态，不将余量用于额外实验。本轮技术准备新增真实模型/JEV/金融claim/actualowner/Langfuse请求均为0。

项目AGENTS.md:11明确“不扩大业务300/模型90/JEV10”，因此600秒不能由旧预算或自主授权默认为批准。只剩一次明确600秒政策批准；取得真实新答复后，才生成独立policy绑定/签名及delegated执行授权，以一个新owner连续完成完整3275对、独立语义复核与完整3660组合，随后一次Langfuse内容/评分/读回，不再逐窗确认。当前没有operation-policy-human-reply、实际policy approval、execution authorization或actual campaign/records/spool。

最终前缀/readiness独审：12a96b0e478d5231cf9e0f41463c626939e8217c965a1da0537c04c08e7912d2，限定范围0未关闭P1/P2，确认110次source/capture及28份readiness引用均完整对应原件；只接受提交一次具体600秒政策批准，actual仍false。原bootstrap cd7审查及c6a readiness字节保持。
