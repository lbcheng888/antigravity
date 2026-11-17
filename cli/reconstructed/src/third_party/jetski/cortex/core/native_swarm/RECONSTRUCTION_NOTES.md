# native_swarm Reconstruction Notes (agy v1.5.10)

Reconstructed from `/Users/mac/.local/bin/agy` (v1.5.10) ARM64 disassembly, `gopclntab` line/inline tables, `__DATA_CONST.__go_type` reflection metadata (`recovered-gotypes.json`), and `__TEXT.__rodata` string literals.

- **Priority files reconstructed:** 13 Go files (covering 262 binary symbols out of 432 total in `third_party/jetski/cortex/core/native_swarm`).
- **Remaining non-priority files (not reconstructed):** `board/feed.go`, `board/ratelimit.go`, `board/validate.go`, `channel_action.go`, `channels.go`, `head_hook.go`, `join.go`, `models.go`, `park_guard_hook.go`, `prompt_worker.go`, `registry.go`, `registry/{hooks,join,registry,unreachable}.go`, `spec.go`, `status.go`, `swarmid.go`, `wait_check_hook.go`.

## Key Recovered Behaviors

### 1. `board.jsonl` & `members.json` Persistence (`board/filestore/filestore.go`, `members_file.go`)
- **Per-path locking:** `sharedFor(dir)` keys a global map by `sharedKey{fs, path}` (checking `reflect.Type.Comparable()`), holding a `sync.RWMutex`, `board.Feed`, and an incremental `controlCache` that scans `board.jsonl` from `scanned` byte offset up to the last `\n`.
- **`board.jsonl` append & dedup:** `appendLocked` allocates `Seq = countLines(raw) + 1`, formats timestamp via `time.Now().Format("2006-01-02 15:04:05")`, marshals with `encoding/json/v2`, appends `\n`, and writes via `fs.Path.AppendFile(..., 0644)`. `Post` and `AddMembers` deduplicate by `RequestID` via `findRequest` (scanning the last 64 lines); `AddMembers` repairs missing members on brief retry (`repairMembersLocked`).
- **`members.json` & legacy fields:** `UpdateMember` and `lockedStore.UpdateMemberLocked` persist `MemberState` (`WakeMode`, `WaitSince`, `LastReadSeq`, `LastNudgedSeq`, `LastParkBlockedSeq`) via `fs.Path.AtomicWrite` (`json.MarshalIndent` with `"  "`). `members_file.go` bridges `board.Member` and legacy `{killed, model, host_instance_id}` fields via `plain` + `legacyMember`.

### 2. `@name`, `@prefix`, `@all`, `@#channel` & Wake Engine (`board/mentions.go`, `board/channel_mentions.go`, `board/channels.go`, `wake/engine.go`)
- **Mention matching:** `board.Mentions` lowercases text and target, checks `named(low, target)` or `named(low, "all")`, and falls back to `CopyPrefix(target)` (stripping trailing `-<digits>` for sharded workers). `named` requires `@` not preceded by `isNameRune` (letter, digit, `-`, `_`) and followed by a non-name rune or EOF.
- **Channel mentions:** `ChannelMentions` extracts `@#<name>` tokens (allowing `"main"` or `ValidateChannelName`), `MentionsIn` matches `Mentions` OR (`len(channels) > 0` and `"*"` or matching channel), and `CheckChannelMentions` emits warnings for unknown/archived channels.
- **Wake decision & `last_read`/`wait_since`:** `wake.Engine.concernedBy` skips messages with `RequestID != ""` (or checks `WakeMode == "anything"` via `WaitSince < msg.Seq && m.Sees` vs `"mentions"` via `WaitSince < msg.Seq && MentionsIn(EffectiveChannels)`). `evaluateLocked` skips `brief` messages for ringing (catching up other live local workers via `CatchUp` + `FastForwardAuthor`), sets `WakeMode = "working"` on wake, and rings idle `"working"` workers on mention (`ifIdle`). `transact` loops optimistically over `store.UpdateMember` (`errStaleSnapshot`, `errNoChange`).

### 3. Quota Pause (`quota_pause.go`)
- Uses package-level `sync.Map` (`quotaPaused`) keyed by member name storing `quotaPause{head, model, traj, seen}`.
- `pauseOnQuota` records the current `s.Head(ctx)` and logs `"native swarm: %s ran out of quota; it sits out until the swarm moves past #%d"`.
- `sitOutForQuota` checks `movedSince` and deletes the entry once the board head advances (or head read fails), logging `"native swarm: %s resumes after its quota pause"`.

### 4. `manage_swarm` & Workspaces `inherit` / `share` / `branch` (`manage_swarm.go`, `post_message.go`, `read_board.go`, `wait.go`)
- **`manage_swarm` actions:** `Execute` dispatches `status`, `models`, `add`, `model`, `kill`, `title`, plus feature-gated `join` and `channel`.
- **Workspace modes & sharding:** `add` resolves `resolveAddPlan` (`n` numbered copies `<name>-1..N` or `tasks` slices), creates workspaces for `share`/`branch` via `forkWorkspace` (rolling back via `removeForks` on partial or `CreateAgent` failure), registers members via `store.AddMembers` (cleaning up on `ErrUnconfirmedAdd`), flushes `wake.Engine`, and dispatches `workerBriefing`.
- **Kill & model switch:** `kill` refuses coordinator (`refuseCoordinatorTarget`), expands numbered groups via `CopyPrefix`, enforces `AddedBy == actor` unless coordinator, splits local vs remote (`splitByOwner` + `killRemote`), calls `store.KillMembers`, `cancelSelfSwitch`, and `AgentManager.KillAgent`. `setModel` supports async self-switch (`resumeSelfWhenIdleAsync`) and managed waiting worker model updates.

## Uncertainties
- Exact `StepRenderInfo` builder details in `manage_swarm.go` (`swarmRenderInfo`, `addRenderInfo`, `killRenderInfo`) and `wait.go` (`swarmWaitRenderInfo` short label branch).
- Exact `movedSince` threshold checks (`cmp #0x65`, `cmp #0xe` in `quota_pause.go`) and `agentmanager.CreateAgentOpts` field mapping in `manage_swarm.go`.
- Exact `board` sentinel error variable names (`ErrNameUsed`, `ErrMemberKilled`, `ErrNotMember`, `ErrClosed`) and `controlCache.get` 10-byte needle literal in `filestore.go`.
- `wake.Engine.transact` `decide` error return path at L460 and `board.roleSuffixRE` exact regex pattern in `board/channels.go`.
- JSON `ParameterSchema()` raw string payloads in `post_message.go`, `read_board.go`, and `wait.go` (lengths 157 B, 870 B, 734 B) were not dumped verbatim.

## Symbol -> File Map (Priority Files)

| File | StartLine | VA | Size (B) | Symbol |
|---|---:|---|---:|---|
| `board/channel_mentions.go` | 19 | `0x101fb2b70` | 880 | `board/board.ChannelMentions` |
| `board/channel_mentions.go` | 57 | `0x101fb2ee0` | 512 | `board/board.MentionsIn` |
| `board/channel_mentions.go` | 81 | `0x101fb30e0` | 816 | `board/board.CheckChannelMentions` |
| `board/channel_mentions.go` | 136 | `0x101fbc3a0` | 272 | `board/board.(*Lifecycle).ActiveAt.func1` |
| `board/channel_mentions.go` | 155 | `0x101fb3410` | 848 | `board/board.ActiveAt` |
| `board/channel_mentions.go` | 174 | `0x101fb3760` | 832 | `board/board.CalledOnIn` |
| `board/channels.go` | 115 | `0x101fb3aa0` | 208 | `board/board.RoleOf` |
| `board/channels.go` | 139 | `0x101fb3b70` | 352 | `board/board.EffectiveChannels` |
| `board/channels.go` | 148 | `0x101fb3cd0` | 96 | `board/board.(*Member).EffectiveChannels` |
| `board/channels.go` | 158 | `0x101fb3d30` | 208 | `board/board.(*Member).HomeChannelIn` |
| `board/channels.go` | 175 | `0x101fb3e00` | 352 | `board/board.SeesIn` |
| `board/channels.go` | 191 | `0x101fb3f60` | 176 | `board/board.(*Member).Sees` |
| `board/channels.go` | 215 | `0x101fb4010` | 1056 | `board/board.ChannelsAt` |
| `board/channels.go` | 235 | `0x101fb4430` | 1296 | `board/board.View.Visible` |
| `board/channels.go` | 269 | `0x101fb4940` | 384 | `board/board.View.Validate` |
| `board/channels.go` | 286 | `0x101fb4ac0` | 384 | `board/board.View.SelectsMain` |
| `board/channels.go` | 304 | `0x101fb4c40` | 640 | `board/board.View.Since` |
| `board/channels.go` | 329 | `0x101fb4ec0` | 800 | `board/board.View.Latest` |
| `board/channels.go` | 349 | `0x101fb51e0` | 416 | `board/board.View.AnyUnread` |
| `board/channels.go` | 374 | `0x101fb5380` | 32 | `board/board.View.Unread` |
| `board/channels.go` | 380 | `0x101fb53a0` | 1264 | `board/board.ApplyControl` |
| `board/channels.go` | 451 | `0x101fb5890` | 576 | `board/board.SubscribeOp` |
| `board/channels.go` | 467 | `0x101fb5ad0` | 5920 | `board/board.PrepareChannelOp` |
| `board/channels.go` | 562 | `0x101fb71f0` | 736 | `board/board.InheritChannels` |
| `board/channels.go` | 575 | `0x101fb74d0` | 416 | `board/board.BriefChannel` |
| `board/filestore/filestore.go` | 46 | `0x101fbceb0` | 240 | `board/filestore/filestore.Exists` |
| `board/filestore/filestore.go` | 103 | `0x101fbcfa0` | 256 | `board/filestore/filestore.keyFor` |
| `board/filestore/filestore.go` | 121 | `0x101fbd0a0` | 464 | `board/filestore/filestore.sharedFor` |
| `board/filestore/filestore.go` | 123 | `0x101fc4930` | 112 | `board/filestore/filestore.sharedFor.deferwrap1` |
| `board/filestore/filestore.go` | 139 | `0x101fbd270` | 16 | `board/filestore/filestore.backend.Kind` |
| `board/filestore/filestore.go` | 145 | `0x101fbd280` | 528 | `board/filestore/filestore.backend.Open` |
| `board/filestore/filestore.go` | 160 | `0x101fbd490` | 2080 | `board/filestore/filestore.backend.Create` |
| `board/filestore/filestore.go` | 164 | `0x101fc4ba0` | 64 | `board/filestore/filestore.backend.Create.deferwrap1` |
| `board/filestore/filestore.go` | 178 | `0x101fc49a0` | 512 | `board/filestore/filestore.backend.Create.func1` |
| `board/filestore/filestore.go` | 225 | `0x101fbdcb0` | 16 | `board/filestore/filestore.(*Store).Dir` |
| `board/filestore/filestore.go` | 229 | `0x101fbdcc0` | 16 | `board/filestore/filestore.(*Store).SwarmID` |
| `board/filestore/filestore.go` | 232 | `0x101fbdcd0` | 16 | `board/filestore/filestore.(*Store).Backend` |
| `board/filestore/filestore.go` | 234 | `0x101fbdce0` | 304 | `board/filestore/filestore.(*Store).checkOpen` |
| `board/filestore/filestore.go` | 236 | `0x101fc4be0` | 112 | `board/filestore/filestore.(*Store).checkOpen.deferwrap1` |
| `board/filestore/filestore.go` | 245 | `0x101fbde10` | 352 | `board/filestore/filestore.(*Store).Close` |
| `board/filestore/filestore.go` | 264 | `0x101fbdf70` | 1984 | `board/filestore/filestore.(*Store).Post` |
| `board/filestore/filestore.go` | 269 | `0x101fc4ca0` | 64 | `board/filestore/filestore.(*Store).Post.deferwrap1` |
| `board/filestore/filestore.go` | 301 | `0x101fc4c50` | 80 | `board/filestore/filestore.(*Store).Post.func1` |
| `board/filestore/filestore.go` | 316 | `0x101fbe730` | 304 | `board/filestore/filestore.(*Store).writeCursors` |
| `board/filestore/filestore.go` | 324 | `0x101fbe860` | 944 | `board/filestore/filestore.(*Store).appendLocked` |
| `board/filestore/filestore.go` | 341 | `0x101fbec10` | 736 | `board/filestore/filestore.(*Store).Since` |
| `board/filestore/filestore.go` | 346 | `0x101fc4ce0` | 128 | `board/filestore/filestore.(*Store).Since.deferwrap1` |
| `board/filestore/filestore.go` | 356 | `0x101fbeef0` | 496 | `board/filestore/filestore.(*Store).Latest` |
| `board/filestore/filestore.go` | 361 | `0x101fc4d60` | 128 | `board/filestore/filestore.(*Store).Latest.deferwrap1` |
| `board/filestore/filestore.go` | 378 | `0x101fbf0e0` | 928 | `board/filestore/filestore.(*Store).Get` |
| `board/filestore/filestore.go` | 383 | `0x101fc4de0` | 128 | `board/filestore/filestore.(*Store).Get.deferwrap1` |
| `board/filestore/filestore.go` | 405 | `0x101fbf480` | 384 | `board/filestore/filestore.(*Store).Head` |
| `board/filestore/filestore.go` | 410 | `0x101fc4e60` | 128 | `board/filestore/filestore.(*Store).Head.deferwrap1` |
| `board/filestore/filestore.go` | 421 | `0x101fbf600` | 864 | `board/filestore/filestore.(*Store).CalledOn` |
| `board/filestore/filestore.go` | 426 | `0x101fc4f60` | 128 | `board/filestore/filestore.(*Store).CalledOn.deferwrap1` |
| `board/filestore/filestore.go` | 437 | `0x101fc4ee0` | 128 | `board/filestore/filestore.(*Store).CalledOn.func1` |
| `board/filestore/filestore.go` | 458 | `0x101fbf960` | 272 | `board/filestore/filestore.(*Store).SinceIn` |
| `board/filestore/filestore.go` | 460 | `0x101fc4fe0` | 576 | `board/filestore/filestore.(*Store).SinceIn.func1` |
| `board/filestore/filestore.go` | 465 | `0x101fc5220` | 176 | `board/filestore/filestore.(*Store).SinceIn.func1.1` |
| `board/filestore/filestore.go` | 474 | `0x101fbfa70` | 256 | `board/filestore/filestore.(*Store).LatestIn` |
| `board/filestore/filestore.go` | 476 | `0x101fc52d0` | 320 | `board/filestore/filestore.(*Store).LatestIn.func1` |
| `board/filestore/filestore.go` | 481 | `0x101fc5410` | 176 | `board/filestore/filestore.(*Store).LatestIn.func1.1` |
| `board/filestore/filestore.go` | 492 | `0x101fbfb70` | 240 | `board/filestore/filestore.(*Store).VisibleAfter` |
| `board/filestore/filestore.go` | 494 | `0x101fc54c0` | 256 | `board/filestore/filestore.(*Store).VisibleAfter.func1` |
| `board/filestore/filestore.go` | 499 | `0x101fc55c0` | 144 | `board/filestore/filestore.(*Store).VisibleAfter.func1.1` |
| `board/filestore/filestore.go` | 510 | `0x101fbfc60` | 880 | `board/filestore/filestore.(*Store).readView` |
| `board/filestore/filestore.go` | 519 | `0x101fc5650` | 128 | `board/filestore/filestore.(*Store).readView.deferwrap1` |
| `board/filestore/filestore.go` | 547 | `0x101fbffd0` | 208 | `board/filestore/filestore.(*Store).filter` |
| `board/filestore/filestore.go` | 571 | `0x101fc00a0` | 352 | `board/filestore/filestore.(*Store).CreateChannel` |
| `board/filestore/filestore.go` | 576 | `0x101fc0200` | 336 | `board/filestore/filestore.(*Store).ArchiveChannel` |
| `board/filestore/filestore.go` | 581 | `0x101fc0350` | 544 | `board/filestore/filestore.(*Store).SubscribeChannels` |
| `board/filestore/filestore.go` | 593 | `0x101fc0570` | 1776 | `board/filestore/filestore.(*Store).channelOp` |
| `board/filestore/filestore.go` | 598 | `0x101fc5720` | 64 | `board/filestore/filestore.(*Store).channelOp.deferwrap1` |
| `board/filestore/filestore.go` | 618 | `0x101fc56d0` | 80 | `board/filestore/filestore.(*Store).channelOp.func1` |
| `board/filestore/filestore.go` | 628 | `0x101fc0c60` | 336 | `board/filestore/filestore.(*Store).Roster` |
| `board/filestore/filestore.go` | 633 | `0x101fc5760` | 128 | `board/filestore/filestore.(*Store).Roster.deferwrap1` |
| `board/filestore/filestore.go` | 642 | `0x101fc0db0` | 2880 | `board/filestore/filestore.(*Store).AddMembers` |
| `board/filestore/filestore.go` | 647 | `0x101fc57e0` | 64 | `board/filestore/filestore.(*Store).AddMembers.deferwrap1` |
| `board/filestore/filestore.go` | 702 | `0x101fc18f0` | 1072 | `board/filestore/filestore.(*Store).repairMembersLocked` |
| `board/filestore/filestore.go` | 736 | `0x101fc1d20` | 128 | `board/filestore/filestore.(*Store).KillMembers` |
| `board/filestore/filestore.go` | 737 | `0x101fc5820` | 448 | `board/filestore/filestore.(*Store).KillMembers.func1` |
| `board/filestore/filestore.go` | 752 | `0x101fc1da0` | 128 | `board/filestore/filestore.(*Store).SetTitle` |
| `board/filestore/filestore.go` | 753 | `0x101fc59e0` | 224 | `board/filestore/filestore.(*Store).SetTitle.func1` |
| `board/filestore/filestore.go` | 767 | `0x101fc1e20` | 144 | `board/filestore/filestore.(*Store).SetMemberModel` |
| `board/filestore/filestore.go` | 768 | `0x101fc5ac0` | 416 | `board/filestore/filestore.(*Store).SetMemberModel.func1` |
| `board/filestore/filestore.go` | 777 | `0x101fc1eb0` | 128 | `board/filestore/filestore.(*Store).ModelApplied` |
| `board/filestore/filestore.go` | 778 | `0x101fc5c60` | 80 | `board/filestore/filestore.(*Store).ModelApplied.func1` |
| `board/filestore/filestore.go` | 785 | `0x101fc1f30` | 592 | `board/filestore/filestore.(*Store).rosterOp` |
| `board/filestore/filestore.go` | 790 | `0x101fc5cb0` | 64 | `board/filestore/filestore.(*Store).rosterOp.deferwrap1` |
| `board/filestore/filestore.go` | 817 | `0x101fc2180` | 1456 | `board/filestore/filestore.(*Store).UpdateMember` |
| `board/filestore/filestore.go` | 822 | `0x101fc5cf0` | 64 | `board/filestore/filestore.(*Store).UpdateMember.deferwrap1` |
| `board/filestore/filestore.go` | 854 | `0x101fc2730` | 448 | `board/filestore/filestore.(*Store).Subscribe` |
| `board/filestore/filestore.go` | 877 | `0x101fc28f0` | 64 | `board/filestore/filestore.(*lockedStore).Names` |
| `board/filestore/filestore.go` | 879 | `0x101fc2930` | 128 | `board/filestore/filestore.(*lockedStore).ChannelActive` |
| `board/filestore/filestore.go` | 881 | `0x101fc29b0` | 240 | `board/filestore/filestore.(*lockedStore).Member` |
| `board/filestore/filestore.go` | 890 | `0x101fc2aa0` | 496 | `board/filestore/filestore.(*lockedStore).UpdateMemberLocked` |
| `board/filestore/filestore.go` | 912 | `0x101fc2c90` | 416 | `board/filestore/filestore.readFeed` |
| `board/filestore/filestore.go` | 926 | `0x101fc2e30` | 128 | `board/filestore/filestore.readMessages` |
| `board/filestore/filestore.go` | 936 | `0x101fc2eb0` | 480 | `board/filestore/filestore.parse` |
| `board/filestore/filestore.go` | 954 | `0x101fc3090` | 176 | `board/filestore/filestore.countLines` |
| `board/filestore/filestore.go` | 966 | `0x101fc3140` | 640 | `board/filestore/filestore.findRequest` |
| `board/filestore/filestore.go` | 992 | `0x101fc33c0` | 656 | `board/filestore/filestore.loadRoster` |
| `board/filestore/filestore.go` | 1025 | `0x101fc3650` | 1536 | `board/filestore/filestore.(*controlCache).get` |
| `board/filestore/filestore.go` | 1027 | `0x101fc5d30` | 112 | `board/filestore/filestore.(*controlCache).get.deferwrap1` |
| `board/filestore/filestore.go` | 1066 | `0x101fc3c50` | 240 | `board/filestore/filestore.loadRosterOrEmpty` |
| `board/filestore/filestore.go` | 1074 | `0x101fc3d40` | 608 | `board/filestore/filestore.decodeRoster` |
| `board/filestore/filestore.go` | 1092 | `0x101fc3fa0` | 1088 | `board/filestore/filestore.writeRoster` |
| `board/filestore/filestore.go` | 1112 | `0x101fc43e0` | 208 | `board/filestore/filestore.updateRoster` |
| `board/filestore/members_file.go` | 34 | `0x101fc44b0` | 304 | `board/filestore/filestore.fileMember.MarshalJSON` |
| `board/filestore/members_file.go` | 48 | `0x101fc45e0` | 336 | `board/filestore/filestore.(*fileMember).UnmarshalJSON` |
| `board/filestore/members_file.go` | 68 | `0x101fc4730` | 512 | `board/filestore/filestore.(*fileMember).member` |
| `board/members.go` | 69 | `0x101fb7aa0` | 384 | `board/board.StateOf` |
| `board/members.go` | 183 | `0x101fb7c20` | 64 | `board/board.(*Member).Killed` |
| `board/members.go` | 187 | `0x101fb7c60` | 144 | `board/board.(*Member).Live` |
| `board/members.go` | 193 | `0x101fb7cf0` | 32 | `board/board.(*Member).Model` |
| `board/members.go` | 202 | `0x101fb7d10` | 304 | `board/board.(*Member).MarkKilled` |
| `board/members.go` | 237 | `0x101fb7e40` | 256 | `board/board.(*Roster).NameOf` |
| `board/mentions.go` | 20 | `0x101fb7f40` | 240 | `board/board.Mentions` |
| `board/mentions.go` | 35 | `0x101fb8030` | 352 | `board/board.CopyPrefix` |
| `board/mentions.go` | 56 | `0x101fb8190` | 400 | `board/board.named` |
| `board/mentions.go` | 93 | `0x101fb8320` | 240 | `board/board.isNameRune` |
| `board/store.go` | 216 | `0x101fb86b0` | 96 | `board/board.(*Member).State` |
| `board/store.go` | 227 | `0x101fb8710` | 160 | `board/board.(*Member).SetState` |
| `board/store.go` | 239 | `0x101fb87b0` | 64 | `board/board.(*MemberState).CatchUp` |
| `board/store.go` | 255 | `0x101fb87f0` | 96 | `board/board.(*MemberState).FastForwardAuthor` |
| `board/store.go` | 265 | `0x101fb8850` | 992 | `board/board.(*Roster).Clone` |
| `board/store.go` | 305 | `0x101fb8c30` | 64 | `board/board.EventKind.String` |
| `manage_swarm.go` | 101 | `0x101fd9f10` | 240 | `nativeswarm.(*manageSwarmTool).boardRef` |
| `manage_swarm.go` | 149 | `0x101fda000` | 992 | `nativeswarm.swarmParameterSchema` |
| `manage_swarm.go` | 338 | `0x101fda3e0` | 688 | `nativeswarm.NewManageSwarmTool` |
| `manage_swarm.go` | 364 | `0x101fda690` | 16 | `nativeswarm.(*manageSwarmTool).Name` |
| `manage_swarm.go` | 369 | `0x101fda6a0` | 432 | `nativeswarm.(*manageSwarmTool).Description` |
| `manage_swarm.go` | 443 | `0x101fda850` | 160 | `nativeswarm.(*manageSwarmTool).ParameterSchema` |
| `manage_swarm.go` | 448 | `0x101fda8f0` | 1632 | `nativeswarm.(*manageSwarmTool).Execute` |
| `manage_swarm.go` | 486 | `0x101fdaf50` | 272 | `nativeswarm.(*manageSwarmTool).bind` |
| `manage_swarm.go` | 498 | `0x101fdb060` | 288 | `nativeswarm.(*manageSwarmTool).swarm` |
| `manage_swarm.go` | 520 | `0x101fdb180` | 480 | `nativeswarm.(*manageSwarmTool).actor` |
| `manage_swarm.go` | 542 | `0x101fdb360` | 1200 | `nativeswarm.(*manageSwarmTool).isCoordinator` |
| `manage_swarm.go` | 563 | `0x101fdb810` | 1168 | `nativeswarm.(*manageSwarmTool).workerSwarm` |
| `manage_swarm.go` | 592 | `0x101fdbca0` | 1152 | `nativeswarm.(*manageSwarmTool).status` |
| `manage_swarm.go` | 626 | `0x101fdc120` | 448 | `nativeswarm.channelsLine` |
| `manage_swarm.go` | 644 | `0x101fdc2e0` | 784 | `nativeswarm.(*manageSwarmTool).inheritParentConfig` |
| `manage_swarm.go` | 680 | `0x101fdc5f0` | 8624 | `nativeswarm.(*manageSwarmTool).add` |
| `manage_swarm.go` | 751 | `0x101ff4f80` | 736 | `nativeswarm.(*manageSwarmTool).add.func1` |
| `manage_swarm.go` | 957 | `0x101fde7a0` | 784 | `nativeswarm.(*manageSwarmTool).addFollowUp` |
| `manage_swarm.go` | 992 | `0x101fdeab0` | 144 | `nativeswarm.(*manageSwarmTool).forkSpec` |
| `manage_swarm.go` | 999 | `0x101fdeb40` | 144 | `nativeswarm.(*manageSwarmTool).removeForks` |
| `manage_swarm.go` | 1008 | `0x101fdebd0` | 2272 | `nativeswarm.(*manageSwarmTool).forkWorkspace` |
| `manage_swarm.go` | 1020 | `0x101ff5260` | 96 | `nativeswarm.(*manageSwarmTool).forkWorkspace.func1` |
| `manage_swarm.go` | 1032 | `0x101fdf4b0` | 304 | `nativeswarm.workspacePath` |
| `manage_swarm.go` | 1047 | `0x101fdf5e0` | 1728 | `nativeswarm.(*manageSwarmTool).models` |
| `manage_swarm.go` | 1084 | `0x101fdfca0` | 576 | `nativeswarm.workerBriefing` |
| `manage_swarm.go` | 1097 | `0x101fdfee0` | 448 | `nativeswarm.(*manageSwarmTool).resolveForkSource` |
| `manage_swarm.go` | 1124 | `0x101fe00a0` | 976 | `nativeswarm.forkStep` |
| `manage_swarm.go` | 1150 | `0x101fe0470` | 992 | `nativeswarm.(*manageSwarmTool).ensureSwarm` |
| `manage_swarm.go` | 1196 | `0x101fe0850` | 3008 | `nativeswarm.(*manageSwarmTool).kill` |
| `manage_swarm.go` | 1299 | `0x101fe1410` | 6688 | `nativeswarm.(*manageSwarmTool).setModel` |
| `manage_swarm.go` | 1387 | `0x101ff5300` | 112 | `nativeswarm.(*manageSwarmTool).setModel.func1` |
| `manage_swarm.go` | 1400 | `0x101ff52c0` | 64 | `nativeswarm.(*manageSwarmTool).setModel.func2` |
| `manage_swarm.go` | 1422 | `0x101fe2e30` | 416 | `nativeswarm.(*manageSwarmTool).runOnModel` |
| `manage_swarm.go` | 1435 | `0x101fe2fd0` | 320 | `nativeswarm.(*manageSwarmTool).modelOverride` |
| `manage_swarm.go` | 1458 | `0x101fe3110` | 464 | `nativeswarm.resumeSelfWhenIdleAsync` |
| `manage_swarm.go` | 1464 | `0x101ff5370` | 160 | `nativeswarm.resumeSelfWhenIdleAsync.func1` |
| `manage_swarm.go` | 1465 | `0x101ff5410` | 128 | `nativeswarm.resumeSelfWhenIdleAsync.func1.1` |
| `manage_swarm.go` | 1475 | `0x101fe32e0` | 160 | `nativeswarm.cancelSelfSwitch` |
| `manage_swarm.go` | 1485 | `0x101fe3380` | 1056 | `nativeswarm.(*manageSwarmTool).resumeSelfWhenIdle` |
| `manage_swarm.go` | 1487 | `0x101ff5490` | 80 | `nativeswarm.(*manageSwarmTool).resumeSelfWhenIdle.deferwrap1` |
| `manage_swarm.go` | 1511 | `0x101fe37a0` | 256 | `nativeswarm.modelSwitchNotice` |
| `manage_swarm.go` | 1520 | `0x101fe38a0` | 272 | `nativeswarm.selfModelSwitchNotice` |
| `manage_swarm.go` | 1533 | `0x101fe39b0` | 864 | `nativeswarm.modelSwitchStep` |
| `manage_swarm.go` | 1562 | `0x101fe3d10` | 1840 | `nativeswarm.(*manageSwarmTool).managerOf` |
| `manage_swarm.go` | 1586 | `0x101fe4440` | 1392 | `nativeswarm.(*manageSwarmTool).refuseCoordinatorTarget` |
| `manage_swarm.go` | 1599 | `0x101fe49b0` | 1376 | `nativeswarm.killTargetNames` |
| `manage_swarm.go` | 1625 | `0x101ff66c0` | 48 | `nativeswarm.killTargetNames.func1` |
| `manage_swarm.go` | 1638 | `0x101fe4f10` | 784 | `nativeswarm.(*manageSwarmTool).title` |
| `manage_swarm.go` | 1675 | `0x101fe5220` | 3024 | `nativeswarm.resolveAddPlan` |
| `manage_swarm.go` | 1732 | `0x101fe5df0` | 608 | `nativeswarm.workerNames` |
| `manage_swarm.go` | 1753 | `0x101fe6050` | 1072 | `nativeswarm.validateWorkerName` |
| `manage_swarm.go` | 1774 | `0x101fe6480` | 352 | `nativeswarm.jsonResult` |
| `manage_swarm.go` | 1782 | `0x101fe65e0` | 736 | `nativeswarm.swarmRenderInfo` |
| `manage_swarm.go` | 1792 | `0x101fe68c0` | 2320 | `nativeswarm.addRenderInfo` |
| `manage_swarm.go` | 1818 | `0x101fe71d0` | 880 | `nativeswarm.killRenderInfo` |
| `manage_swarm.go` | 1834 | `0x101fe7540` | 1488 | `nativeswarm.statusMarkdown` |
| `manage_swarm.go` | 1874 | `0x101fe7b10` | 544 | `nativeswarm.modelsMarkdown` |
| `manage_swarm.go` | 1886 | `0x101fe7d30` | 160 | `nativeswarm.blockquote` |
| `post_message.go` | 73 | `0x101feb090` | 288 | `nativeswarm.NewPostMessageTool` |
| `post_message.go` | 82 | `0x101feb1b0` | 16 | `nativeswarm.(*postMessageTool).Name` |
| `post_message.go` | 87 | `0x101feb1c0` | 16 | `nativeswarm.(*postMessageTool).Description` |
| `post_message.go` | 93 | `0x101feb1d0` | 208 | `nativeswarm.(*postMessageTool).ParameterSchema` |
| `post_message.go` | 103 | `0x101feb2a0` | 1616 | `nativeswarm.(*postMessageTool).Execute` |
| `post_message.go` | 161 | `0x101feb8f0` | 384 | `nativeswarm.(*postMessageTool).channel` |
| `post_message.go` | 175 | `0x101feba70` | 1040 | `nativeswarm.postMessageRenderInfo` |
| `post_message.go` | 199 | `0x101febe80` | 192 | `nativeswarm.memberRun` |
| `quota_pause.go` | 37 | `0x101fec5b0` | 736 | `nativeswarm.pauseOnQuota` |
| `quota_pause.go` | 55 | `0x101fec890` | 576 | `nativeswarm.sitOutForQuota` |
| `quota_pause.go` | 77 | `0x101fecad0` | 688 | `nativeswarm.movedSince` |
| `read_board.go` | 91 | `0x101fecd80` | 288 | `nativeswarm.NewReadBoardTool` |
| `read_board.go` | 100 | `0x101fecea0` | 16 | `nativeswarm.(*readBoardTool).Name` |
| `read_board.go` | 105 | `0x101feceb0` | 16 | `nativeswarm.(*readBoardTool).Description` |
| `read_board.go` | 113 | `0x101fecec0` | 208 | `nativeswarm.(*readBoardTool).ParameterSchema` |
| `read_board.go` | 121 | `0x101fecf90` | 2352 | `nativeswarm.(*readBoardTool).Execute` |
| `read_board.go` | 217 | `0x101fed8c0` | 1840 | `nativeswarm.(*readBoardTool).readView` |
| `read_board.go` | 288 | `0x101fedff0` | 528 | `nativeswarm.channelNames` |
| `read_board.go` | 302 | `0x101fee200` | 1936 | `nativeswarm.readBoardRenderInfo` |
| `read_board.go` | 336 | `0x101fee990` | 1488 | `nativeswarm.(*readBoardTool).status` |
| `read_board.go` | 358 | `0x101feef60` | 1056 | `nativeswarm.statusTable` |
| `wait.go` | 60 | `0x101ff3790` | 288 | `nativeswarm.NewSwarmWaitTool` |
| `wait.go` | 69 | `0x101ff38b0` | 16 | `nativeswarm.(*swarmWaitTool).Name` |
| `wait.go` | 74 | `0x101ff38c0` | 16 | `nativeswarm.(*swarmWaitTool).Description` |
| `wait.go` | 85 | `0x101ff38d0` | 208 | `nativeswarm.(*swarmWaitTool).ParameterSchema` |
| `wait.go` | 93 | `0x101ff39a0` | 832 | `nativeswarm.(*swarmWaitTool).Execute` |
| `wait.go` | 133 | `0x101ff3ce0` | 480 | `nativeswarm.(*swarmWaitTool).anythingIn` |
| `wait.go` | 153 | `0x101ff3ec0` | 672 | `nativeswarm.swarmWaitRenderInfo` |
| `wake/engine.go` | 109 | `0x101ff5cf0` | 96 | `wake/wake.WithIdleProbe.1` |
| `wake/engine.go` | 116 | `0x101fca230` | 672 | `wake/wake.New` |
| `wake/engine.go` | 136 | `0x101fca4d0` | 96 | `wake/wake.(*Engine).Close` |
| `wake/engine.go` | 146 | `0x101fca530` | 656 | `wake/wake.(*Engine).ensureInit` |
| `wake/engine.go` | 188 | `0x101fca7c0` | 368 | `wake/wake.concernedBy` |
| `wake/engine.go` | 203 | `0x101fca930` | 624 | `wake/wake.(*Engine).onEvent` |
| `wake/engine.go` | 232 | `0x101fcaba0` | 2896 | `wake/wake.(*Engine).evaluateLocked` |
| `wake/engine.go` | 240 | `0x101fcd400` | 80 | `wake/wake.(*Engine).evaluateLocked.func1` |
| `wake/engine.go` | 254 | `0x101fcd3b0` | 80 | `wake/wake.(*Engine).evaluateLocked.func2` |
| `wake/engine.go` | 277 | `0x101fcd360` | 80 | `wake/wake.(*Engine).evaluateLocked.func3` |
| `wake/engine.go` | 281 | `0x101fced70` | 112 | `wake/wake.(*Engine).evaluateLocked.func4` |
| `wake/engine.go` | 309 | `0x101fcb6f0` | 544 | `wake/wake.(*Engine).enqueue` |
| `wake/engine.go` | 319 | `0x101fcd450` | 64 | `wake/wake.(*Engine).enqueue.gowrap1` |
| `wake/engine.go` | 326 | `0x101fcb910` | 704 | `wake/wake.(*Engine).Flush` |
| `wake/engine.go` | 328 | `0x101fcd490` | 112 | `wake/wake.(*Engine).Flush.deferwrap1` |
| `wake/engine.go` | 352 | `0x101fcbbd0` | 240 | `wake/wake.(*Engine).loadHead` |
| `wake/engine.go` | 354 | `0x101fcd500` | 112 | `wake/wake.(*Engine).loadHead.deferwrap1` |
| `wake/engine.go` | 402 | `0x101fcbcc0` | 1856 | `wake/wake.(*Engine).transact` |
| `wake/engine.go` | 407 | `0x101fcd900` | 176 | `wake/wake.(*Engine).transact.func1` |
| `wake/engine.go` | 411 | `0x101fcd8b0` | 80 | `wake/wake.(*Engine).transact.func2` |
| `wake/engine.go` | 431 | `0x101fcd7a0` | 272 | `wake/wake.(*Engine).transact.func3` |
| `wake/engine.go` | 446 | `0x101fcd650` | 336 | `wake/wake.(*Engine).transact.func4` |
| `wake/engine.go` | 464 | `0x101fcd570` | 224 | `wake/wake.(*Engine).transact.func5` |
| `wake/engine.go` | 488 | `0x101fcc400` | 112 | `wake/wake.gone` |
| `wake/engine.go` | 496 | `0x101fcc470` | 656 | `wake/wake.(*Engine).Park` |
| `wake/engine.go` | 498 | `0x101fcd9b0` | 128 | `wake/wake.(*Engine).Park.func1` |
| `wake/engine.go` | 525 | `0x101fcc700` | 368 | `wake/wake.(*Engine).MarkRead` |
| `wake/engine.go` | 526 | `0x101fcda30` | 48 | `wake/wake.(*Engine).MarkRead.func1` |
| `wake/engine.go` | 551 | `0x101fcc870` | 592 | `wake/wake.(*Engine).MarkSeen` |
| `wake/engine.go` | 561 | `0x101fcda60` | 1632 | `wake/wake.(*Engine).MarkSeen.func1` |
| `wake/engine.go` | 605 | `0x101fce0c0` | 32 | `wake/wake.(*Engine).MarkSeen.func1.1` |
| `wake/engine.go` | 634 | `0x101fccac0` | 256 | `wake/wake.(*Engine).Nudge` |
| `wake/engine.go` | 635 | `0x101fce0e0` | 512 | `wake/wake.(*Engine).Nudge.func1` |
| `wake/engine.go` | 641 | `0x101fced00` | 112 | `wake/wake.(*Engine).Nudge.func1.1` |
| `wake/engine.go` | 665 | `0x101fce2e0` | 128 | `wake/wake.(*Engine).Nudge.func1.2` |
| `wake/engine.go` | 687 | `0x101fccbc0` | 192 | `wake/wake.(*Engine).HasConcerning` |
| `wake/engine.go` | 689 | `0x101fce360` | 336 | `wake/wake.(*Engine).HasConcerning.func1` |
| `wake/engine.go` | 706 | `0x101fcec90` | 112 | `wake/wake.(*Engine).HasConcerning.func1.1` |
| `wake/engine.go` | 743 | `0x101fccc80` | 848 | `wake/wake.(*Engine).ParkOnStop` |
| `wake/engine.go` | 745 | `0x101fce4b0` | 560 | `wake/wake.(*Engine).ParkOnStop.func1` |
| `wake/engine.go` | 764 | `0x101fce770` | 32 | `wake/wake.(*Engine).ParkOnStop.func1.1` |
| `wake/engine.go` | 779 | `0x101fce6e0` | 144 | `wake/wake.(*Engine).ParkOnStop.func1.2` |
| `wake/engine.go` | 842 | `0x101fccfd0` | 912 | `wake/wake.(*Engine).CheckStop` |
| `wake/engine.go` | 851 | `0x101fce790` | 944 | `wake/wake.(*Engine).CheckStop.func1` |
| `wake/engine.go` | 883 | `0x101fcec70` | 32 | `wake/wake.(*Engine).CheckStop.func1.1` |
| `wake/engine.go` | 885 | `0x101fcec50` | 32 | `wake/wake.(*Engine).CheckStop.func1.2` |
| `wake/engine.go` | 891 | `0x101fcebd0` | 128 | `wake/wake.(*Engine).CheckStop.func1.3` |
| `wake/engine.go` | 904 | `0x101fcebb0` | 32 | `wake/wake.(*Engine).CheckStop.func1.4` |
| `wake/engine.go` | 912 | `0x101fceb40` | 112 | `wake/wake.(*Engine).CheckStop.func1.5` |
