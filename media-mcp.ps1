param(
    [string]$Config = "config.yml"
)

# 加载 .env 文件
$envFile = ".env"
if (Test-Path $envFile) {
    Write-Host "[media-mcp] 加载环境变量: $envFile"
    Get-Content $envFile | ForEach-Object {
        if ($_ -match "^\s*([^#][^=]+)=(.*)") {
            $key = $Matches[1].Trim()
            $value = $Matches[2].Trim().Trim('"').Trim("'")
            [System.Environment]::SetEnvironmentVariable($key, $value)
        }
    }
} else {
    Write-Warning "[media-mcp] 未找到 $envFile 文件，请复制 .env.example 为 .env 并填写 API Key"
}

Write-Host "[media-mcp] 启动 media-mcp..."
Write-Host "[media-mcp] 配置: $Config"
Write-Host ""

if (-not (Test-Path "build/media-mcp.exe")) {
    Write-Error "[media-mcp] 错误: 未找到二进制文件 build/media-mcp.exe，请先运行 make build"
    exit 1
}

Write-Host "[media-mcp] 运行 build/media-mcp.exe ..."
& .\build\media-mcp.exe --config $Config
