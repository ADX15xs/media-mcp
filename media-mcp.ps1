param(
    [switch]$Build,
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

# 检查 Go
$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Write-Error "[media-mcp] 错误: 未找到 Go 编译器"
    exit 1
}

if ($Build) {
    Write-Host "[media-mcp] 构建中..."
    go build -o media-mcp.exe ./cmd/media-mcp/
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[media-mcp] 构建成功! 正在运行..."
        .\media-mcp.exe --config $Config
    } else {
        Write-Error "[media-mcp] 构建失败"
        exit 1
    }
} else {
    Write-Host "[media-mcp] 运行开发模式..."
    go run ./cmd/media-mcp/ --config $Config
}
