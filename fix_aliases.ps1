function Process-File {
    param($filePath, $replacements)
    
    if (-not (Test-Path $filePath)) {
        Write-Host "MISSING: $filePath"
        return
    }
    
    $content = [System.IO.File]::ReadAllText($filePath, [System.Text.Encoding]::UTF8)
    $original = $content
    
    foreach ($r in $replacements) {
        $content = [regex]::Replace($content, $r[0], $r[1])
    }
    
    if ($content -ne $original) {
        [System.IO.File]::WriteAllText($filePath, $content, [System.Text.Encoding]::UTF8)
        Write-Host "CHANGED: $filePath"
    }
}

# Group A: mdl -> model
$mdlFiles = @(
    "D:\Codes\qiulin\moss\appkit\extensions.go",
    "D:\Codes\qiulin\moss\apps\mosscode\commands_exec.go",
    "D:\Codes\qiulin\moss\appkit\serve_test.go",
    "D:\Codes\qiulin\moss\agent\registry_test.go",
    "D:\Codes\qiulin\moss\agent\store_test.go",
    "D:\Codes\qiulin\moss\agent\task.go",
    "D:\Codes\qiulin\moss\apps\mosscode\runtime_support.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\prometheus\observer_test.go",
    "D:\Codes\qiulin\moss\apps\mosswork\chatservice.go",
    "D:\Codes\qiulin\moss\agent\tools_helpers.go",
    "D:\Codes\qiulin\moss\agent\tools_test.go",
    "D:\Codes\qiulin\moss\userio\attachments\attachments.go",
    "D:\Codes\qiulin\moss\contrib\tui\app.go",
    "D:\Codes\qiulin\moss\knowledge\store.go",
    "D:\Codes\qiulin\moss\knowledge\memory_test.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\otel\observer_test.go",
    "D:\Codes\qiulin\moss\knowledge\memory.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat_slash_core.go",
    "D:\Codes\qiulin\moss\knowledge\manager.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat.go",
    "D:\Codes\qiulin\moss\contrib\tui\app_update_chat.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\context_test.go",
    "D:\Codes\qiulin\moss\gateway\gateway.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat_update.go",
    "D:\Codes\qiulin\moss\appkit\runtime\context_manager.go",
    "D:\Codes\qiulin\moss\appkit\runtime\context.go",
    "D:\Codes\qiulin\moss\testing\mock_llm.go",
    "D:\Codes\qiulin\moss\appkit\repl.go",
    "D:\Codes\qiulin\moss\knowledge\adapters\memory_test.go",
    "D:\Codes\qiulin\moss\knowledge\adapters\memory.go",
    "D:\Codes\qiulin\moss\appkit\runtime\scheduled_runner.go",
    "D:\Codes\qiulin\moss\testing\eval\types.go",
    "D:\Codes\qiulin\moss\testing\eval\runner.go",
    "D:\Codes\qiulin\moss\testing\eval\judge.go",
    "D:\Codes\qiulin\moss\testing\eval\eval_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\knowledge.go",
    "D:\Codes\qiulin\moss\appkit\product\inspect_test.go",
    "D:\Codes\qiulin\moss\examples\websocket\main.go",
    "D:\Codes\qiulin\moss\appkit\product\pricing.go",
    "D:\Codes\qiulin\moss\appkit\product\pricing_test.go",
    "D:\Codes\qiulin\moss\examples\mossresearch\main.go",
    "D:\Codes\qiulin\moss\examples\mosswriter\main.go",
    "D:\Codes\qiulin\moss\kernel\checkpoint.go",
    "D:\Codes\qiulin\moss\examples\mossroom\room.go",
    "D:\Codes\qiulin\moss\kernel\checkpoint_test.go",
    "D:\Codes\qiulin\moss\kernel\kernel.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_helpers.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop.go",
    "D:\Codes\qiulin\moss\kernel\feature_packages_test.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_bench_test.go",
    "D:\Codes\qiulin\moss\kernel\kernel_test.go",
    "D:\Codes\qiulin\moss\kernel\loop\execution_plan_test.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_run.go",
    "D:\Codes\qiulin\moss\kernel\loop\execution_plan.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_llm.go",
    "D:\Codes\qiulin\moss\kernel\session\thread_metadata.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_tools.go",
    "D:\Codes\qiulin\moss\kernel\loop\turn_plan.go",
    "D:\Codes\qiulin\moss\kernel\session\store_file_test.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_tool_events.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_test.go",
    "D:\Codes\qiulin\moss\kernel\session\store_file.go",
    "D:\Codes\qiulin\moss\kernel\session\session_test.go",
    "D:\Codes\qiulin\moss\kernel\session\context_test.go",
    "D:\Codes\qiulin\moss\kernel\session\session.go",
    "D:\Codes\qiulin\moss\kernel\session\context.go",
    "D:\Codes\qiulin\moss\kernel\session\prompt_context_test.go",
    "D:\Codes\qiulin\moss\kernel\session\prompt_context.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\patch_tool_calls.go",
    "D:\Codes\qiulin\moss\kernel\session\manager.go",
    "D:\Codes\qiulin\moss\kernel\session\hooks.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\priority.go",
    "D:\Codes\qiulin\moss\kernel\option.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\rag.go",
    "D:\Codes\qiulin\moss\kernel\metrics\metrics_test.go",
    "D:\Codes\qiulin\moss\kernel\metrics\observer.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\sliding.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\summarize.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\truncate.go",
    "D:\Codes\qiulin\moss\providers\builder.go",
    "D:\Codes\qiulin\moss\providers\failover.go",
    "D:\Codes\qiulin\moss\presets\deepagent\deepagent_test.go",
    "D:\Codes\qiulin\moss\providers\embedding\openai.go",
    "D:\Codes\qiulin\moss\providers\failover_test.go",
    "D:\Codes\qiulin\moss\kernel\observe\observer.go",
    "D:\Codes\qiulin\moss\providers\claude\claude.go",
    "D:\Codes\qiulin\moss\providers\claude\claude_test.go",
    "D:\Codes\qiulin\moss\providers\router.go",
    "D:\Codes\qiulin\moss\providers\gemini\gemini_test.go",
    "D:\Codes\qiulin\moss\providers\gemini\gemini.go",
    "D:\Codes\qiulin\moss\providers\router_test.go",
    "D:\Codes\qiulin\moss\providers\openai\openai_test.go",
    "D:\Codes\qiulin\moss\providers\openai\openai.go"
)

foreach ($f in $mdlFiles) {
    Process-File $f @(
        @('(?m)^(\s+)mdl\s+"github\.com/mossagents/moss/kernel/model"', '$1"github.com/mossagents/moss/kernel/model"'),
        @('\bmdl\.', 'model.')
    )
}

# Group A: kobs -> observe
$kobsFiles = @(
    "D:\Codes\qiulin\moss\apps\mosscode\commands_exec.go",
    "D:\Codes\qiulin\moss\apps\mosscode\config.go",
    "D:\Codes\qiulin\moss\apps\mosscode\runtime_support.go",
    "D:\Codes\qiulin\moss\appkit\serve_test.go",
    "D:\Codes\qiulin\moss\appkit\serve.go",
    "D:\Codes\qiulin\moss\contrib\tui\app.go",
    "D:\Codes\qiulin\moss\appkit\product\trace.go",
    "D:\Codes\qiulin\moss\appkit\product\state_observer.go",
    "D:\Codes\qiulin\moss\appkit\product\pricing_test.go",
    "D:\Codes\qiulin\moss\appkit\product\observer.go",
    "D:\Codes\qiulin\moss\appkit\product\inspect_test.go",
    "D:\Codes\qiulin\moss\appkit\product\inspect_p2.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\prometheus\observer_test.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\prometheus\observer.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\otel\observer_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\statestore_test.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\otel\observer.go",
    "D:\Codes\qiulin\moss\appkit\runtime\statestore.go",
    "D:\Codes\qiulin\moss\sandbox\git_snapshot_test.go",
    "D:\Codes\qiulin\moss\sandbox\git_snapshot.go",
    "D:\Codes\qiulin\moss\appkit\runtime\events\events_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\events\events.go",
    "D:\Codes\qiulin\moss\contrib\tui\progress_test.go",
    "D:\Codes\qiulin\moss\kernel\option.go",
    "D:\Codes\qiulin\moss\contrib\tui\progress.go",
    "D:\Codes\qiulin\moss\kernel\kernel_test.go",
    "D:\Codes\qiulin\moss\kernel\extension_bridge.go",
    "D:\Codes\qiulin\moss\kernel\kernel.go",
    "D:\Codes\qiulin\moss\kernel\checkpoint.go",
    "D:\Codes\qiulin\moss\kernel\metrics\observer.go",
    "D:\Codes\qiulin\moss\kernel\checkpoint\file_store.go",
    "D:\Codes\qiulin\moss\kernel\metrics\metrics_test.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_helpers.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_llm.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_run.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_test.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_tools.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_tool_events.go",
    "D:\Codes\qiulin\moss\kernel\hooks\events.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\audit.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\policy.go"
)

foreach ($f in $kobsFiles) {
    Process-File $f @(
        @('(?m)^(\s+)kobs\s+"github\.com/mossagents/moss/kernel/observe"', '$1"github.com/mossagents/moss/kernel/observe"'),
        @('\bkobs\.', 'observe.')
    )
}

# Group A: memstore -> memory
$memstoreFiles = @(
    "D:\Codes\qiulin\moss\appkit\runtime\statestore_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\statestore.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_store_workspace.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_store_sqlite.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_records.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_pipeline.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_indexed_store.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory.go",
    "D:\Codes\qiulin\moss\appkit\runtime\context_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\context_manager.go",
    "D:\Codes\qiulin\moss\appkit\runtime\context.go"
)

foreach ($f in $memstoreFiles) {
    Process-File $f @(
        @('(?m)^(\s+)memstore\s+"github\.com/mossagents/moss/kernel/memory"', '$1"github.com/mossagents/moss/kernel/memory"'),
        @('\bmemstore\.', 'memory.')
    )
}

# Group A: kws/kkws -> workspace
$kwsFiles = @(
    "D:\Codes\qiulin\moss\agent\tools_collab.go",
    "D:\Codes\qiulin\moss\agent\tools.go",
    "D:\Codes\qiulin\moss\appkit\product\change_runtime.go",
    "D:\Codes\qiulin\moss\contrib\tui\app_session_ops.go",
    "D:\Codes\qiulin\moss\appkit\product\change_ops.go",
    "D:\Codes\qiulin\moss\appkit\runtime\builtintools.go",
    "D:\Codes\qiulin\moss\appkit\runtime\builtintools_test.go",
    "D:\Codes\qiulin\moss\appkit\product\runtime.go",
    "D:\Codes\qiulin\moss\appkit\product\runtime_review.go",
    "D:\Codes\qiulin\moss\appkit\product\runtime_test.go",
    "D:\Codes\qiulin\moss\testing\mock_sandbox.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_store_workspace.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory.go",
    "D:\Codes\qiulin\moss\appkit\runtime\execution_policy.go",
    "D:\Codes\qiulin\moss\appkit\runtime\runtime.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_pipeline.go",
    "D:\Codes\qiulin\moss\appkit\runtime\execution_surface.go",
    "D:\Codes\qiulin\moss\appkit\runtime\context_test.go",
    "D:\Codes\qiulin\moss\sandbox\git_patch_test.go",
    "D:\Codes\qiulin\moss\skill\skill.go",
    "D:\Codes\qiulin\moss\sandbox\git_patch_journal.go",
    "D:\Codes\qiulin\moss\sandbox\git_revert_test.go",
    "D:\Codes\qiulin\moss\sandbox\git_repo.go",
    "D:\Codes\qiulin\moss\apps\mosscode\commands_recovery.go",
    "D:\Codes\qiulin\moss\sandbox\git_patch.go",
    "D:\Codes\qiulin\moss\sandbox\git_repo_test.go",
    "D:\Codes\qiulin\moss\sandbox\git_snapshot_test.go",
    "D:\Codes\qiulin\moss\sandbox\git_snapshot.go",
    "D:\Codes\qiulin\moss\sandbox\isolation_local.go",
    "D:\Codes\qiulin\moss\sandbox\local.go",
    "D:\Codes\qiulin\moss\sandbox\local_test.go",
    "D:\Codes\qiulin\moss\sandbox\memory.go",
    "D:\Codes\qiulin\moss\sandbox\noop.go",
    "D:\Codes\qiulin\moss\sandbox\docker\sandbox_test.go",
    "D:\Codes\qiulin\moss\sandbox\docker\sandbox.go",
    "D:\Codes\qiulin\moss\sandbox\sandbox.go",
    "D:\Codes\qiulin\moss\sandbox\scoped.go",
    "D:\Codes\qiulin\moss\kernel\checkpoint.go",
    "D:\Codes\qiulin\moss\kernel\checkpoint_test.go",
    "D:\Codes\qiulin\moss\sandbox\objectstore\workspace.go",
    "D:\Codes\qiulin\moss\kernel\kernel_test.go",
    "D:\Codes\qiulin\moss\kernel\kernel.go",
    "D:\Codes\qiulin\moss\kernel\option.go"
)

foreach ($f in $kwsFiles) {
    Process-File $f @(
        @('(?m)^(\s+)kws\s+"github\.com/mossagents/moss/kernel/workspace"', '$1"github.com/mossagents/moss/kernel/workspace"'),
        @('(?m)^(\s+)kkws\s+"github\.com/mossagents/moss/kernel/workspace"', '$1"github.com/mossagents/moss/kernel/workspace"'),
        @('\bkws\.', 'workspace.'),
        @('\bkkws\.', 'workspace.')
    )
}

# Group A: ckpt -> checkpoint
$ckptFiles = @(
    "D:\Codes\qiulin\moss\apps\mosscode\root.go",
    "D:\Codes\qiulin\moss\apps\mosscode\main_test.go",
    "D:\Codes\qiulin\moss\apps\mosscode\commands_recovery.go",
    "D:\Codes\qiulin\moss\contrib\tui\app_session_ops.go",
    "D:\Codes\qiulin\moss\contrib\tui\app_runtime_posture.go",
    "D:\Codes\qiulin\moss\presets\deepagent\deepagent.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat_test.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat_slash_handlers_ops.go",
    "D:\Codes\qiulin\moss\appkit\runtime\statestore.go",
    "D:\Codes\qiulin\moss\contrib\tui\thread_pickers.go",
    "D:\Codes\qiulin\moss\kernel\session\lineage.go",
    "D:\Codes\qiulin\moss\appkit\product\change_runtime.go",
    "D:\Codes\qiulin\moss\kernel\session\lineage_test.go",
    "D:\Codes\qiulin\moss\kernel\checkpoint.go",
    "D:\Codes\qiulin\moss\kernel\checkpoint_test.go",
    "D:\Codes\qiulin\moss\appkit\product\inspect_test.go",
    "D:\Codes\qiulin\moss\appkit\product\inspect_p2.go",
    "D:\Codes\qiulin\moss\appkit\product\inspect_p1.go",
    "D:\Codes\qiulin\moss\kernel\kernel_test.go",
    "D:\Codes\qiulin\moss\appkit\product\runtime.go",
    "D:\Codes\qiulin\moss\kernel\kernel.go",
    "D:\Codes\qiulin\moss\appkit\product\runtime_test.go",
    "D:\Codes\qiulin\moss\kernel\option.go",
    "D:\Codes\qiulin\moss\appkit\product\runtime_threads.go"
)

foreach ($f in $ckptFiles) {
    Process-File $f @(
        @('(?m)^(\s+)ckpt\s+"github\.com/mossagents/moss/kernel/checkpoint"', '$1"github.com/mossagents/moss/kernel/checkpoint"'),
        @('\bckpt\.', 'checkpoint.')
    )
}

Write-Host "Group A done."

# Group B: intr -> io (kernel package)
$intrFiles = @(
    "D:\Codes\qiulin\moss\appkit\appkit_test.go",
    "D:\Codes\qiulin\moss\apps\mosswork\chatservice.go",
    "D:\Codes\qiulin\moss\apps\mosswork\wailsio.go",
    "D:\Codes\qiulin\moss\appkit\serve_test.go",
    "D:\Codes\qiulin\moss\apps\mosscode\runtime_support.go",
    "D:\Codes\qiulin\moss\examples\basic\main.go",
    "D:\Codes\qiulin\moss\examples\custom-tool\main.go",
    "D:\Codes\qiulin\moss\examples\websocket\main.go",
    "D:\Codes\qiulin\moss\appkit\runtime_builder.go",
    "D:\Codes\qiulin\moss\apps\mosscode\commands_exec.go",
    "D:\Codes\qiulin\moss\mcp\mcp_test.go",
    "D:\Codes\qiulin\moss\mcp\mcp.go",
    "D:\Codes\qiulin\moss\examples\mosswriter\main.go",
    "D:\Codes\qiulin\moss\examples\mossclaw\main.go",
    "D:\Codes\qiulin\moss\examples\mossquant\main.go",
    "D:\Codes\qiulin\moss\examples\mossroom\room.go",
    "D:\Codes\qiulin\moss\skill\skill.go",
    "D:\Codes\qiulin\moss\examples\mossresearch\main.go",
    "D:\Codes\qiulin\moss\appkit\runtime\statestore_test.go",
    "D:\Codes\qiulin\moss\contrib\tui\userio.go",
    "D:\Codes\qiulin\moss\appkit\runtime\scheduled_runner.go",
    "D:\Codes\qiulin\moss\appkit\runtime\scheduled_io.go",
    "D:\Codes\qiulin\moss\userio\approval\approval.go",
    "D:\Codes\qiulin\moss\appkit\runtime\runtime_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\runtime.go",
    "D:\Codes\qiulin\moss\appkit\runtime\execution_policy.go",
    "D:\Codes\qiulin\moss\testing\mock_io.go",
    "D:\Codes\qiulin\moss\contrib\tui\statusline.go",
    "D:\Codes\qiulin\moss\appkit\runtime\events\events_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\events\events.go",
    "D:\Codes\qiulin\moss\appkit\runtime\memory_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\context_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\builtintools_test.go",
    "D:\Codes\qiulin\moss\appkit\runtime\builtintools.go",
    "D:\Codes\qiulin\moss\contrib\tui\progress.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat.go",
    "D:\Codes\qiulin\moss\contrib\tui\ask_form.go",
    "D:\Codes\qiulin\moss\contrib\tui\app_test.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat_test.go",
    "D:\Codes\qiulin\moss\kernel\feature_packages_test.go",
    "D:\Codes\qiulin\moss\kernel\option.go",
    "D:\Codes\qiulin\moss\contrib\tui\app.go",
    "D:\Codes\qiulin\moss\contrib\tui\chat_components.go",
    "D:\Codes\qiulin\moss\kernel\hooks\events.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\audit.go",
    "D:\Codes\qiulin\moss\kernel\observe\events.go",
    "D:\Codes\qiulin\moss\kernel\workspace\workspace.go",
    "D:\Codes\qiulin\moss\appkit\product\trace_test.go",
    "D:\Codes\qiulin\moss\appkit\product\trace.go",
    "D:\Codes\qiulin\moss\kernel\observe\observer.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\prometheus\observer_test.go",
    "D:\Codes\qiulin\moss\appkit\product\approval_test.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\prometheus\observer.go",
    "D:\Codes\qiulin\moss\appkit\product\approval.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\policy.go",
    "D:\Codes\qiulin\moss\sandbox\local_test.go",
    "D:\Codes\qiulin\moss\sandbox\local.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\rbac.go",
    "D:\Codes\qiulin\moss\appkit\builder.go",
    "D:\Codes\qiulin\moss\kernel\observe\execution_event.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\otel\observer_test.go",
    "D:\Codes\qiulin\moss\sandbox\git_snapshot_test.go",
    "D:\Codes\qiulin\moss\contrib\telemetry\otel\observer.go",
    "D:\Codes\qiulin\moss\appkit\product\exec_test.go",
    "D:\Codes\qiulin\moss\appkit\product\exec.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_tool_events.go",
    "D:\Codes\qiulin\moss\kernel\kernel_test.go",
    "D:\Codes\qiulin\moss\kernel\kernel_boot_test.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_test.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop.go",
    "D:\Codes\qiulin\moss\kernel\kernel.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_run.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_llm.go",
    "D:\Codes\qiulin\moss\presets\deepagent\deepagent_test.go",
    "D:\Codes\qiulin\moss\presets\deepagent\deepagent.go",
    "D:\Codes\qiulin\moss\kernel\session\approval_state.go"
)

foreach ($f in $intrFiles) {
    if (-not (Test-Path $f)) {
        Write-Host "MISSING: $f"
        continue
    }
    
    $content = [System.IO.File]::ReadAllText($f, [System.Text.Encoding]::UTF8)
    $original = $content
    
    # Check if file imports both kernel/io (as intr) and stdlib "io"
    # Detect stdlib "io" import: a standalone `"io"` that is NOT part of a longer path
    $hasKernelIO = $content -match 'intr\s+"github\.com/mossagents/moss/kernel/io"'
    $hasStdlibIO = $content -match '(?m)^\s+"io"\s*$'
    
    if ($hasKernelIO) {
        if ($hasStdlibIO) {
            # Rename alias from intr to kernio
            $content = [regex]::Replace($content, '(?m)^(\s+)intr(\s+"github\.com/mossagents/moss/kernel/io")', '$1kernio$2')
            $content = [regex]::Replace($content, '\bintr\.', 'kernio.')
        } else {
            # Remove alias, use "io." directly
            $content = [regex]::Replace($content, '(?m)^(\s+)intr\s+"github\.com/mossagents/moss/kernel/io"', '$1"github.com/mossagents/moss/kernel/io"')
            $content = [regex]::Replace($content, '\bintr\.', 'io.')
        }
    }
    
    if ($content -ne $original) {
        [System.IO.File]::WriteAllText($f, $content, [System.Text.Encoding]::UTF8)
        Write-Host "CHANGED: $f"
    }
}

Write-Host "Group B (intr) done."

# Group B: kerrors -> errors (kernel package)
$kerrorsFiles = @(
    "D:\Codes\qiulin\moss\appkit\runtime\runtime.go",
    "D:\Codes\qiulin\moss\kernel\kernel_test.go",
    "D:\Codes\qiulin\moss\kernel\kernel.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_llm.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_test.go",
    "D:\Codes\qiulin\moss\providers\failover.go",
    "D:\Codes\qiulin\moss\kernel\loop\loop_tool_events.go",
    "D:\Codes\qiulin\moss\kernel\run_supervisor.go",
    "D:\Codes\qiulin\moss\kernel\hooks\builtins\policy.go",
    "D:\Codes\qiulin\moss\kernel\run_supervisor_test.go",
    "D:\Codes\qiulin\moss\mcp\mcp.go",
    "D:\Codes\qiulin\moss\mcp\mcp_test.go"
)

foreach ($f in $kerrorsFiles) {
    if (-not (Test-Path $f)) {
        Write-Host "MISSING: $f"
        continue
    }
    
    $content = [System.IO.File]::ReadAllText($f, [System.Text.Encoding]::UTF8)
    $original = $content
    
    $hasKernelErrors = $content -match 'kerrors\s+"github\.com/mossagents/moss/kernel/errors"'
    $hasStdlibErrors = $content -match '(?m)^\s+"errors"\s*$'
    
    if ($hasKernelErrors) {
        if ($hasStdlibErrors) {
            # Keep kerrors alias unchanged
            Write-Host "KEEP kerrors (conflict): $f"
        } else {
            # Remove alias, use "errors." directly
            $content = [regex]::Replace($content, '(?m)^(\s+)kerrors\s+"github\.com/mossagents/moss/kernel/errors"', '$1"github.com/mossagents/moss/kernel/errors"')
            $content = [regex]::Replace($content, '\bkerrors\.', 'errors.')
        }
    }
    
    if ($content -ne $original) {
        [System.IO.File]::WriteAllText($f, $content, [System.Text.Encoding]::UTF8)
        Write-Host "CHANGED: $f"
    }
}

Write-Host "Group B (kerrors) done."
Write-Host "All replacements complete."
