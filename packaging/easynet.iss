; Inno Setup 脚本：生成带标准卸载项（unins000.exe）的安装包。
; 版本号由 CI 通过 /DMyAppVersion=<tag> 注入，本地默认 0.4.5。

#ifndef MyAppVersion
  #define MyAppVersion "0.4.5"
#endif

#define MyAppName "easynet"
#define MyAppPublisher "love486love"
#define MyAppURL "https://github.com/love486love/easynet"
#define MyAppExeName "easynet.exe"

[Setup]
AppId={{6D3E9B52-8C1F-4A7D-9E44-2B7A5F0C1D88}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
DefaultDirName={autopf}\easynet
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\{#MyAppExeName}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
OutputDir=..\dist-installer
OutputBaseFilename=easynet-setup-{#MyAppVersion}

[Languages]
Name: "chinesesimplified"; MessagesFile: "ChineseSimplified.isl"

[Files]
Source: "..\dist\easynet.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\checksums.txt"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\卸载 {#MyAppName}"; Filename: "{uninstallexe}"

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "启动 {#MyAppName}"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{cmd}"; Parameters: "/c reg delete ""HKCU\Software\Microsoft\Windows\CurrentVersion\Run"" /v easynet /f"; Flags: runhidden
Filename: "{cmd}"; Parameters: "/c taskkill /IM easynet.exe /F"; Flags: runhidden

[UninstallDelete]
Type: filesandordirs; Name: "{localappdata}\easynet"

[Code]
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    MsgBox('卸载完成。' + #13#10 + #13#10 +
      '如果安装目录中还有残留文件（例如 unins000.exe、unins000.dat 或 log 文件夹），' +
      '可以直接把整个安装文件夹删除。', mbInformation, MB_OK);
end;
