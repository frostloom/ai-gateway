# 计费压测汇总（面试用）

这些数字都是本机闭环压测，模拟模型延迟是 0，64 个租户，一个计费进程。不能当成生产容量，也不是和 New API 同时跑出来的。

原始目录还在 `/Users/a1/项目/ai-gateway/eval/project-load-2026-11-02/gateway-open-load-runs/`，没有删。

## 现在采用的方案

请求只等 Redis 扣钱。预扣一条明细、结算一条明细，后台送进 Kafka。大约每 5 秒把账单和余额变动写进 MySQL。Kafka 不改余额。

采用的一次完整结果是 `20261108-145621`。四档账都平，发送失败重试表是 0。

| 并发 | 每秒成功 | p50 | 说明 |
|---:|---:|---:|---|
| 1 | 141.7 | 4.68 ms | 账平 |
| 4 | 406.5 | 7.57 ms | 账平 |
| 16 | 964.4 | 13.20 ms | 账平 |
| 64 | 1198.5 | 45.61 ms | 账平，27175 笔结算 |

更早一次本机 New API：并发 1 是 232.2 次/秒，4 是 596.4，16 是 694.8，64 是 767.7。那次 Postgres 把 `synchronous_commit` 关了。不能和上表当成同一次对比。

## 没有采用的做法

- `20261108-013010`：MySQL 提交后再抄一份进 Kafka。并发 64 是 2344.3 次/秒，账平。请求路径上没有等 Kafka。

- `20261108-132904`：明细在请求里入队，但发送仍按大约 200 毫秒攒。并发 64 是 1755.4 次/秒，账平。

- `20261108-195201`：让消费者写 MySQL，失败也等消费后再退 Redis。并发 1 是 97.0，4 是 377.8，账平。并发 16 请求约 839 次/秒，6 秒后 MySQL 没写完，程序停止，并发 64 没开始。

- `20261108-000331`：还没把明细放到 Kafka 上。并发 64 是 644.4 次/秒。

## 全部有结果的目录

下面每一行是一个目录里记录到的各档速度。`停` 表示目录里有 error.txt。账不平只在当时的审计文件里，这里用最后一档审计的不一致租户数。

| 目录 | 模式 | 各档每秒成功 | 最后审计不一致 | 停 |
|---|---|---|---:|---|
| 20261103-223533 | disabled | None→39, None→93, None→137 | 0 |  |
| 20261103-223640 | disabled | None→40, None→40, None→99, None→99, None→141, None→147 | 0 |  |
| 20261103-224112 | disabled | None→140, None→147, None→129, None→137, None→133, None→137 | 0 |  |
| 20261103-224641 | disabled | None→140, None→138, None→137, None→136, None→129, None→126 | 0 |  |
| 20261103-225339 | disabled | None→139, None→139, None→179, None→178, None→178, None→131 | 0 |  |
| 20261103-230033 | disabled | None→10 | 0 |  |
| 20261103-230133 | disabled | None→198, None→193 | 0 |  |
| 20261103-230422 | disabled | None→204, None→167 | 0 |  |
| 20261104-003950 | disabled | None→39, None→93, None→126 | 0 |  |
| 20261104-004050 | disabled | None→100, None→99, None→138, None→134 | 0 |  |
| 20261104-004235 | disabled | None→99, None→99, None→135, None→130 | 0 |  |
| 20261104-004818 | disabled | None→39, None→95, None→143 | 0 |  |
| 20261104-004913 | disabled | None→100, None→100, None→130, None→135 | 0 |  |
| 20261104-011815 | disabled | None→99, None→100, None→138, None→132 | 0 |  |
| 20261104-012234 | disabled | None→130, None→136 | 0 |  |
| 20261104-012608 | disabled | None→20 | 0 |  |
| 20261104-012703 | disabled | None→140, None→138 | 0 |  |
| 20261104-021400 | disabled | None→99, None→99, None→139, None→139 | 0 |  |
| 20261104-021551 | disabled | None→100, None→99, None→138, None→138 | 0 |  |
| 20261104-021829 | disabled | None→19, None→15, None→60, None→11 | 0 |  |
| 20261104-022016 | disabled | None→30, None→30, None→25, None→42 | 0 |  |
| 20261104-022329 | disabled | None→30, None→29, None→61, None→39 | 0 |  |
| 20261104-024738 | disabled | None→99, None→100, None→136, None→136 | 0 |  |
| 20261104-024937 | disabled | None→99, None→99, None→138, None→138 | 0 |  |
| 20261104-042840 | disabled | None→99, None→100, None→138, None→136, None→143, None→139, None→134, None→122 | 0 |  |
| 20261104-043353 | disabled | None→99, None→100, None→137, None→138, None→149, None→148, None→158, None→158 | 0 |  |
| 20261104-044333 | disabled | None→97, None→97, None→135, None→134, None→140, None→138, None→128, None→137 | 0 |  |
| 20261104-044903 | disabled | None→53, None→53, None→88, None→87, None→120, None→121, None→135, None→132 | 0 |  |
| 20261104-050149 | disabled | None→60, None→99 | 0 |  |
| 20261104-052354 | disabled | None→39 | 0 |  |
| 20261104-052455 | disabled | None→99, None→99, None→138, None→133 | 0 |  |
| 20261104-052614 | disabled | None→99, None→99, None→138, None→136 | 0 |  |
| 20261104-052810 | disabled | None→99, None→98, None→137, None→135 | 0 |  |
| 20261104-060908 | disabled | None→99, None→99, None→134, None→135, None→143, None→139 | 0 |  |
| 20261104-061108 | disabled | None→99, None→99, None→132, None→135, None→138, None→135 | 0 |  |
| 20261104-062728 | disabled | None→92, None→95, None→116, None→130, None→123, None→123 | 0 |  |
| 20261104-083721 | disabled | None→99, None→99, None→139, None→139, None→145, None→147, None→144, None→145 | 0 |  |
| 20261104-084151 | disabled | None→53, None→53, None→88, None→88, None→121, None→117, None→131, None→124 | 0 |  |
| 20261104-085517 | disabled | None→156, None→152, None→147, None→138 | 0 |  |
| 20261104-085727 | disabled | None→143, None→149, None→148, None→136 | 0 |  |
| 20261104-224746 | disabled | None→77, None→4, None→54, None→66, None→43, None→45 | 0 |  |
| 20261104-225834 | disabled | None→38, None→12, None→48, None→52 | 0 |  |
| 20261104-230050 | disabled | None→93, None→97, None→90, None→63 | 0 |  |
| 20261104-230325 | disabled | None→27, None→41, None→38, None→87 | 0 |  |
| 20261104-230745 | disabled | None→99, None→94, None→48, None→88 | 0 |  |
| 20261104-230941 | disabled | None→95, None→97 | 0 |  |
| 20261104-231357 | disabled | None→72, None→70, None→63, None→81, None→44, None→81 | 0 |  |
| 20261104-232028 | disabled | None→79, None→79, None→82, None→96, None→87, None→82 | 0 |  |
| 20261104-232235 | disabled | None→75, None→73, None→111, None→73, None→70, None→91 | 0 |  |
| 20261104-234629 | disabled | None→40, None→75, None→85, None→98 | 0 |  |
| 20261104-234816 | disabled | None→79, None→79, None→117, None→78 | 0 |  |
| 20261105-005136 | disabled | None→123, None→132, None→144, None→134 | 0 |  |
| 20261105-005414 | disabled | None→139, None→134, None→145, None→143 | 0 |  |
| 20261105-010537 | disabled | None→136, None→137, None→157, None→148 | 0 |  |
| 20261105-012229 | disabled | None→20, None→0, None→19 | 0 | 是 |
| 20261105-012414 | disabled | None→20, None→0, None→19 | 0 |  |
| 20261105-012704 | disabled | None→20, None→0, None→15 | 0 |  |
| 20261105-012933 | disabled | None→20, None→0, None→19 | 0 |  |
| 20261105-013055 | disabled | None→20, None→0, None→18 | 0 |  |
| 20261105-013200 | disabled | None→20, None→0, None→18 | 0 |  |
| 20261105-021650 | disabled | None→50, None→49, None→99, None→99, None→132, None→134 | 0 |  |
| 20261105-021942 | disabled | None→118, None→122 | 0 |  |
| 20261105-022348 | disabled | None→133, None→132 | 0 |  |
| 20261105-025351 | disabled | None→100, None→100 | 0 |  |
| 20261105-040926 | disabled | None→96, None→99, None→133, None→134 | 0 |  |
| 20261105-041109 | disabled | None→99, None→99, None→131, None→134 | 0 |  |
| 20261105-052104 | disabled | None→80, None→80, None→119, None→119 | 0 |  |
| 20261105-052314 | disabled | None→80, None→80, None→117, None→118 | 0 |  |
| 20261105-052653 | disabled | None→120, None→111, None→101, None→97 | 0 |  |
| 20261105-053201 | disabled | None→120, None→120, None→114, None→106 | 0 |  |
| 20261105-054156 | disabled | None→120, None→118, None→115, None→107 | 0 |  |
| 20261105-062342 | disabled | None→39 | 0 |  |
| 20261105-062443 | disabled | None→119, None→120, None→118, None→113 | 0 |  |
| 20261105-062942 | disabled | None→120, None→115, None→121, None→116 | 0 |  |
| 20261105-063716 | disabled | None→146, None→133 | 0 |  |
| 20261105-064014 | disabled | None→144, None→128 | 0 |  |
| 20261105-064800 | disabled | None→39 | 0 |  |
| 20261105-064848 | disabled | None→142, None→117 | 0 |  |
| 20261105-065148 | disabled | None→150, None→135 | 0 |  |
| 20261105-065705 | disabled | None→151, None→144 | 0 |  |
| 20261105-071205 | disabled | None→20 | 0 |  |
| 20261105-071504 | disabled | None→20 | 0 |  |
| 20261105-071623 | disabled | None→20 | 0 |  |
| 20261105-073138 | disabled | None→20 | 0 |  |
| 20261105-073230 | disabled | None→20 | 0 |  |
| 20261105-094326 | disabled | None→20 | 0 |  |
| 20261105-095133 | disabled | None→77, None→55, None→65, None→51 | 0 |  |
| 20261105-095402 | disabled | None→55, None→55, None→70, None→57 | 0 |  |
| 20261105-100121 | disabled | None→33, None→62 | 0 |  |
| 20261105-101241 | disabled | None→80, None→72, None→108, None→108 | 0 |  |
| 20261105-101444 | disabled | None→77, None→56, None→62, None→57 | 0 |  |
| 20261105-101921 | disabled | None→73, None→79, None→109, None→104 | 0 |  |
| 20261105-102624 | disabled | None→77, None→78, None→108, None→112 | 0 |  |
| 20261105-103415 | disabled | None→73, None→78, None→62, None→76 | 0 |  |
| 20261105-104413 | disabled | None→38, None→52, None→85, None→58 | 0 |  |
| 20261105-105032 | disabled | None→23 | 0 |  |
| 20261105-110055 | disabled | None→20 | 0 |  |
| 20261105-110240 | disabled | None→20 | 0 |  |
| 20261105-110702 | disabled | None→10 | 0 |  |
| 20261105-110831 | disabled | None→78, None→79 | 0 |  |
| 20261105-114019 | disabled | None→5 | 0 |  |
| 20261105-114137 | disabled | None→5 | 0 |  |
| 20261105-114222 | disabled | None→5 | 0 |  |
| 20261105-114334 | disabled | None→6, None→6 | 0 |  |
| 20261105-115315 | disabled | None→4 | 0 |  |
| 20261105-115503 | disabled | None→6 | 0 |  |
| 20261105-135845 | disabled | None→9, None→8 | 0 |  |
| 20261105-140307 | disabled | None→10, None→10, None→10 | 0 |  |
| 20261105-140824 | disabled | None→19, None→19 | 0 |  |
| 20261105-141236 | disabled | None→18 | 0 |  |
| 20261105-141807 | disabled | None→20, None→20, None→20 | 0 |  |
| 20261105-142113 | disabled | None→40, None→30 | 0 |  |
| 20261105-142546 | disabled | None→39, None→25, None→38 | 0 |  |
| 20261105-143142 | disabled | None→39, None→35 | 0 |  |
| 20261105-143331 | disabled | None→37, None→40 | 0 |  |
| 20261105-144416 | disabled | None→40, None→32 | 0 |  |
| 20261105-145823 | disabled | None→40, None→40, None→53 | 0 |  |
| 20261105-151238 | disabled | None→37, None→40, None→79, None→79 | 0 |  |
| 20261105-152033 | disabled | None→19, None→0, None→18 | 0 |  |
| 20261105-153054 | disabled | None→79, None→74 | 0 |  |
| 20261105-153222 | disabled | None→79, None→79 | 0 |  |
| 20261105-153952 | disabled | None→79, None→79 | 0 |  |
| 20261105-155607 | disabled | None→79, None→79, None→115, None→114 | 0 |  |
| 20261105-155846 | disabled | None→127, None→84 | 0 |  |
| 20261105-160225 | disabled | None→136 | 0 |  |
| 20261105-160442 | disabled | None→137, None→130 | 0 |  |
| 20261105-160938 | disabled | None→134, None→132 | 0 |  |
| 20261105-161216 | disabled | None→131 | 0 |  |
| 20261105-161640 | disabled | None→114 | 0 |  |
| 20261105-163822 | disabled | None→20 | 0 |  |
| 20261105-170236 | disabled | None→18 | 0 |  |
| 20261105-170448 | disabled | None→20 | 0 |  |
| 20261105-170722 | disabled | None→20 | 0 |  |
| 20261105-170923 | disabled | None→20 | 0 |  |
| 20261105-171151 | disabled | None→20 | 0 |  |
| 20261105-171457 | disabled | None→20 | 0 |  |
| 20261105-172042 | disabled | None→118, None→118, None→154, None→158 | 0 |  |
| 20261105-172248 | disabled | None→111, None→119, None→129, None→131 | 0 |  |
| 20261105-172901 | disabled | None→118, None→116, None→150, None→125 | 0 |  |
| 20261105-173459 | disabled | None→128, None→112 | 0 |  |
| 20261105-174306 | disabled | None→66 | 0 |  |
| 20261107-111143 | disabled | None→132, None→120 | 0 |  |
| 20261107-111418 | disabled | None→133 | 0 |  |
| 20261107-121546 | disabled | None→92 | 0 |  |
| 20261107-122042 | disabled | None→92 | 0 |  |
| 20261107-123003 | disabled | None→107 | 0 |  |
| 20261107-123744 | disabled | None→58 | 0 |  |
| 20261107-124741 | disabled | None→91 | 0 |  |
| 20261107-125027 | disabled | None→100 | 0 |  |
| 20261107-125359 | disabled | None→94 | 0 |  |
| 20261107-130625 | disabled | None→157, None→168 | 0 |  |
| 20261107-131102 | disabled | None→156 | 0 |  |
| 20261107-131358 | disabled | None→159, None→191 | 0 |  |
| 20261107-131624 | disabled | None→158, None→186 | 0 |  |
| 20261107-132546 | disabled | None→158 | 0 |  |
| 20261107-133306 | disabled | None→159, None→238 | 0 |  |
| 20261107-174403 | disabled | None→233 | 0 |  |
| 20261107-174543 | disabled | None→79 | 0 |  |
| 20261107-180044 | disabled | 1→13, 4→36, 16→72, 64→123 | 0 |  |
| 20261107-200413 | disabled | 1→25, 4→92 | 0 | 是 |
| 20261107-200946 | disabled | 1→26 |  | 是 |
| 20261107-201628 | disabled | 1→11, 4→84, 16→105, 64→112 | 0 |  |
| 20261107-203227 | disabled | 1→36, 4→128, 16→210 | 0 | 是 |
| 20261107-203613 | disabled | 1→38, 4→104, 16→261, 64→485 | 0 |  |
| 20261107-233437 | disabled | 1→54, 4→78, 16→227, 64→365 | 0 |  |
| 20261107-234104 | disabled | 1→30, 4→106, 16→422, 64→749 | 0 |  |
| 20261108-000331 | disabled | 1→86, 4→243, 16→499, 64→644 | 0 |  |
| 20261108-003240 | direct-after-commit | 1→11, 4→92, 16→239, 64→214 | 0 |  |
| 20261108-004454 | direct-after-commit | 1→24, 4→53, 16→192, 64→409 | 0 | 是 |
| 20261108-004935 | direct-after-commit | 64→233 | 0 |  |
| 20261108-005318 | direct-after-commit | 64→328 | 0 |  |
| 20261108-005614 | direct-after-commit | 64→345 | 0 |  |
| 20261108-010339 | direct-after-commit | 1→28, 4→80, 16→289, 64→451 | 0 |  |
| 20261108-010807 | disabled | 1→35, 4→96, 16→288, 64→448 | 0 | 是 |
| 20261108-011620 | direct-after-commit | 1→30, 4→154, 16→303, 64→306 | 0 |  |
| 20261108-012146 | direct-after-commit | 64→506 | 0 |  |
| 20261108-012800 | direct-after-commit | 64→2237 | 0 |  |
| 20261108-013010 | direct-after-commit | 1→238, 4→735, 16→1590, 64→2344 | 0 |  |
| 20261108-102954 | ledger-queue | 1→166, 4→433 | 0 | 是 |
| 20261108-103658 | ledger-queue | 1→29, 4→319 | 0 | 是 |
| 20261108-132904 | request-async | 1→200, 4→594, 16→1109, 64→1755 | 0 |  |
| 20261108-140032 | request-async | 1→192, 4→608, 16→1318, 64→1687 | 0 |  |
| 20261108-144857 | request-async | 1→126, 4→469, 16→939, 64→828 | 0 | 是 |
| 20261108-145621 | request-async | 1→142, 4→406, 16→964, 64→1199 | 0 |  |
| 20261108-195201 | ledger-queue | 1→97, 4→378, 16→839 | 0 | 是 |
