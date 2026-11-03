; Inno Setup script for the Windows installer (Keel-Setup-x64.exe).
; The release workflow runs it like this:
;   iscc /DAppVersion=1.2.3 /DAppVersionNumeric=1.2.3 packaging\windows\keel.iss
; Paths below are relative to this file.

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef AppVersionNumeric
  #define AppVersionNumeric "0.0.0"
#endif
#ifndef SourceExe
  #define SourceExe "..\..\dist\windows\keel.exe"
#endif

[Setup]
; AppId identifies Keel for upgrades and uninstall. Never change it.
AppId={{CEFB47CF-4FEE-4F67-8EBE-DEFFD9AC6963}
AppName=Keel
AppVersion={#AppVersion}
VersionInfoVersion={#AppVersionNumeric}
AppPublisher=Kody Verhulp
AppPublisherURL=https://github.com/Tosccony/keel
AppSupportURL=https://github.com/Tosccony/keel/issues
DefaultDirName={autopf}\Keel
DefaultGroupName=Keel
DisableProgramGroupPage=yes
; Admin rights, because Keel will install its background service.
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\..\dist
OutputBaseFilename=Keel-Setup-x64
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayIcon={app}\keel.exe

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "keel.exe"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Keel"; Filename: "{app}\keel.exe"
Name: "{autodesktop}\Keel"; Filename: "{app}\keel.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\keel.exe"; Description: "{cm:LaunchProgram,Keel}"; Flags: nowait postinstall skipifsilent
