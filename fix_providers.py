import re
import sys

def process_file(filepath, alias, pkg_name, conflict_param_funcs, io_var_funcs=None):
    """
    Process a Go file to:
    1. Remove alias from import
    2. Replace alias. -> pkg_name.
    3. For functions in conflict_param_funcs: rename `model string` param to `modelName string`
       and replace variable usages inside those functions
    """
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original = content
    
    # Find and rename conflicting function parameters
    # We'll process function by function
    for func_name in (conflict_param_funcs or []):
        # Find the function definition and its body
        # Match: func funcName(... , model string) ... {
        # We need to rename: param `model` -> `modelName` within this function only
        
        # Find function signature
        func_start_pattern = rf'func {re.escape(func_name)}\('
        m = re.search(func_start_pattern, content)
        if not m:
            print(f"  WARNING: function {func_name} not found in {filepath}")
            continue
        
        # Find the opening brace of the function body
        pos = m.start()
        depth = 0
        in_func_sig = True
        brace_start = None
        brace_end = None
        
        for i in range(pos, len(content)):
            if content[i] == '{':
                if in_func_sig:
                    brace_start = i
                    in_func_sig = False
                depth += 1
            elif content[i] == '}':
                depth -= 1
                if depth == 0:
                    brace_end = i
                    break
        
        if brace_start is None or brace_end is None:
            print(f"  WARNING: could not find body of {func_name}")
            continue
        
        # Extract function signature and body
        func_sig = content[pos:brace_start]
        func_body = content[brace_start:brace_end+1]
        
        # Rename param in signature: `, model string)` -> `, modelName string)`
        # Also handle `provider, model, role string`
        new_sig = re.sub(r',\s*model\s+string\)', ', modelName string)', func_sig)
        new_sig = re.sub(r'\bprovider,\s*model,\s*role\s+string\b', 'provider, modelName, role string', new_sig)
        new_sig = re.sub(r'\brole,\s*model\s+string\)', 'role, modelName string)', new_sig)
        
        # Rename variable usage inside function body
        # `model` as a variable (not followed by `.`)
        # But we need to be careful: `model` in the body refers to the parameter
        # while `alias.Xxx` refers to the package (will be renamed later)
        # Pattern: `model` not followed by `.` and not preceded by `c.` (struct field access)
        new_body = re.sub(r'(?<!\.)(?<!\w)\bmodel\b(?!\.)', 'modelName', func_body)
        
        # Replace in content
        content = content[:pos] + new_sig + new_body + content[brace_end+1:]
    
    # Handle io variable renaming for mcp.go
    for func_name in (io_var_funcs or []):
        func_start_pattern = rf'func.*?\b{re.escape(func_name)}\('
        m = re.search(func_start_pattern, content)
        if not m:
            print(f"  WARNING: function {func_name} not found in {filepath}")
            continue
        
        pos = m.start()
        depth = 0
        in_func_sig = True
        brace_start = None
        brace_end = None
        
        for i in range(pos, len(content)):
            if content[i] == '{':
                if in_func_sig:
                    brace_start = i
                    in_func_sig = False
                depth += 1
            elif content[i] == '}':
                depth -= 1
                if depth == 0:
                    brace_end = i
                    break
        
        if brace_start is None or brace_end is None:
            continue
        
        func_sig = content[pos:brace_start]
        func_body = content[brace_start:brace_end+1]
        
        # Rename `io alias.UserIO` -> `userIO alias.UserIO` in signature
        new_sig = re.sub(r'\bio\s+' + re.escape(alias) + r'\.UserIO\b', f'userIO {alias}.UserIO', func_sig)
        
        # In body: rename `io.` -> `userIO.` (method calls on the variable)
        # But keep `alias.` references (package) unchanged
        # The distinction: variable method calls vs package type references
        # Method `Ask` is called on the variable; `InputField`, etc. are package types
        # Replace `io.Ask(` -> `userIO.Ask(` specifically
        new_body = re.sub(r'\bio\.Ask\(', 'userIO.Ask(', func_body)
        # Also rename `if io ==` -> `if userIO ==`
        new_body = re.sub(r'\bio\s*==\s*nil', 'userIO == nil', new_body)
        
        content = content[:pos] + new_sig + new_body + content[brace_end+1:]
    
    # Remove alias from import line
    content = re.sub(
        r'(?m)^(\s+)' + re.escape(alias) + r'\s+"github\.com/mossagents/moss/kernel/' + re.escape(pkg_name) + r'"',
        r'\1"github.com/mossagents/moss/kernel/' + pkg_name + r'"',
        content
    )
    
    # Replace alias. -> pkg_name.
    content = re.sub(r'\b' + re.escape(alias) + r'\.', pkg_name + '.', content)
    
    if content != original:
        with open(filepath, 'w', encoding='utf-8') as f:
            f.write(content)
        print(f"FIXED: {filepath}")
    else:
        print(f"NO CHANGE: {filepath}")


# Process claude.go
process_file(
    r'providers\claude\claude.go',
    alias='mdl',
    pkg_name='model',
    conflict_param_funcs=[
        'toAnthropicMessages',
        'toAnthropicUserBlocks',
        'toAnthropicToolResultBlocks',
        'contentPartsToTextOnlyString',
    ]
)

# Process openai.go
process_file(
    r'providers\openai\openai.go',
    alias='mdl',
    pkg_name='model',
    conflict_param_funcs=[
        'toOpenAIMessages',
        'toOpenAISystemTextParts',
        'toOpenAIUserParts',
        'toOpenAIInputAudioPart',
        'toOpenAIInputVideoPart',
        'toAssistantMessage',
        'contentPartsToTextOnlyString',
    ]
)

# Process gemini.go
process_file(
    r'providers\gemini\gemini.go',
    alias='mdl',
    pkg_name='model',
    conflict_param_funcs=[
        'toGeminiUserParts',
        'toGeminiAssistantParts',
        'toGeminiToolResultParts',
        'toGeminiMediaPart',
        'toGeminiFunctionResponsePart',
        'contentPartsToTextOnlyString',
    ]
)

# Process mcp.go
# kerrors stays (has stdlib errors conflict), intr -> io with variable rename
process_file(
    r'mcp\mcp.go',
    alias='intr',
    pkg_name='io',
    conflict_param_funcs=[],
    io_var_funcs=['buildEnv', 'resolveMCPRequiredEnv']
)

print("Done.")
