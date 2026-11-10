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
; The runtimes Keel ships (scripts/fetch-runtimes.sh windows-x86_64 ...),
; and Microsoft's Visual C++ runtime, which Apache and PHP need.
#ifndef RuntimeDir
  #define RuntimeDir "..\..\dist\runtimes\windows-x86_64"
#endif
#ifndef VCRedist
  #define VCRedist "..\..\dist\vc_redist.x64.exe"
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
SetupIconFile=keel.ico
; keel.exe has no icon of its own yet (that needs a resource tool), so the
; shortcuts and Apps & features use keel.ico. The window sets its own icon.
UninstallDisplayIcon={app}\keel.ico

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "keel.exe"; Flags: ignoreversion
Source: "keel.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RuntimeDir}\mariadb\*"; DestDir: "{app}\mariadb"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#RuntimeDir}\apache\*"; DestDir: "{app}\apache"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#RuntimeDir}\php\*"; DestDir: "{app}\php"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#RuntimeDir}\phpmyadmin\*"; DestDir: "{app}\phpmyadmin"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#VCRedist}"; DestDir: "{tmp}"; Flags: deleteafterinstall

[Icons]
Name: "{autoprograms}\Keel"; Filename: "{app}\keel.exe"; IconFilename: "{app}\keel.ico"
Name: "{autodesktop}\Keel"; Filename: "{app}\keel.exe"; IconFilename: "{app}\keel.ico"; Tasks: desktopicon

[Run]
; Installs or updates Microsoft's Visual C++ runtime; does nothing if a newer
; one is there.
Filename: "{tmp}\vc_redist.x64.exe"; Parameters: "/install /quiet /norestart"; StatusMsg: "Installing the Microsoft Visual C++ runtime..."; Flags: waituntilterminated
Filename: "{app}\keel.exe"; Description: "{cm:LaunchProgram,Keel}"; Flags: nowait postinstall skipifsilent
