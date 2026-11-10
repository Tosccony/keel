<?php
// Keel's welcome page. Keel put it here because this folder was empty.
// Delete it, or replace it with your own index.php, whenever you like.

$port = $_SERVER['SERVER_PORT'] ?? '80';
$inbox = $_SERVER['KEEL_MAIL_INBOX'] ?? '';
$dbPort = (int) ($_SERVER['KEEL_MARIADB_PORT'] ?? 3306);

if (isset($_GET['phpinfo'])) {
    phpinfo();
    exit;
}

// Can PHP reach MariaDB? (root, no password: Keel's default, like XAMPP.)
$db = null;
$dbError = '';
if (extension_loaded('mysqli')) {
    mysqli_report(MYSQLI_REPORT_OFF);
    $conn = @new mysqli('127.0.0.1', 'root', '', '', $dbPort);
    if ($conn->connect_errno) {
        $dbError = $conn->connect_error;
    } else {
        $db = $conn->server_info;
        $conn->close();
    }
}

$projects = array_values(array_filter(scandir(__DIR__), function ($f) {
    return $f[0] !== '.' && is_dir(__DIR__ . '/' . $f);
}));
?><!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Keel is running</title>
<style>
  :root { --bg:#f3f5f8; --card:#fff; --line:#e0e4ea; --text:#161b22; --muted:#5f6977; --primary:#0f766e; --ok:#047857; --warn:#b45309; }
  @media (prefers-color-scheme: dark) { :root { --bg:#0f1216; --card:#171c23; --line:#252c35; --text:#e6eaf0; --muted:#8c96a4; --primary:#2dd4bf; --ok:#34d399; --warn:#fbbf24; } }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--bg); color: var(--text); font: 15px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
  main { max-width: 760px; margin: 0 auto; padding: 40px 20px; }
  h1 { font-size: 26px; margin: 0 0 4px; }
  .lead { color: var(--muted); margin: 0 0 28px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; margin-bottom: 28px; }
  .card { background: var(--card); border: 1px solid var(--line); border-radius: 10px; padding: 16px; }
  .card h2 { font-size: 15px; margin: 0 0 6px; }
  .card p { margin: 0; color: var(--muted); font-size: 14px; }
  a { color: var(--primary); }
  .ok { color: var(--ok); } .warn { color: var(--warn); }
  code { background: var(--card); border: 1px solid var(--line); border-radius: 4px; padding: 1px 5px; font-size: 13px; }
  ul { padding-left: 20px; }
</style>
</head>
<body>
<main>
  <h1>Keel is running</h1>
  <p class="lead">PHP <?= htmlspecialchars(PHP_VERSION) ?> on Apache, at <code>http://localhost:<?= htmlspecialchars($port) ?></code></p>

  <div class="grid">
    <div class="card">
      <h2><a href="/phpmyadmin/">phpMyAdmin</a></h2>
      <p>Manage your MariaDB databases.</p>
    </div>
    <div class="card">
      <h2><?php if ($inbox): ?><a href="<?= htmlspecialchars($inbox) ?>">Mail inbox</a><?php else: ?>Mail inbox<?php endif ?></h2>
      <p>Every email your PHP code sends lands here.</p>
    </div>
    <div class="card">
      <h2><a href="?phpinfo">PHP settings</a></h2>
      <p>Everything <code>phpinfo()</code> reports.</p>
    </div>
    <div class="card">
      <h2>MariaDB</h2>
      <?php if ($db): ?>
        <p class="ok">Connected to MariaDB <?= htmlspecialchars($db) ?> on port <?= $dbPort ?> as root.</p>
      <?php else: ?>
        <p class="warn">Not reachable<?= $dbError ? ': ' . htmlspecialchars($dbError) : '' ?>. Start MariaDB in Keel.</p>
      <?php endif ?>
    </div>
  </div>

  <h2>Your projects</h2>
  <p>Put each project in a folder next to this page. A folder called <code>shop</code> opens at
     <code>http://localhost:<?= htmlspecialchars($port) ?>/shop</code>.</p>
  <?php if ($projects): ?>
    <ul><?php foreach ($projects as $p): ?><li><a href="/<?= rawurlencode($p) ?>/"><?= htmlspecialchars($p) ?></a></li><?php endforeach ?></ul>
  <?php else: ?>
    <p class="lead">No projects yet.</p>
  <?php endif ?>

  <p class="lead">Connect from PHP with <code>new mysqli("localhost", "root", "", "your_database")</code>.
     This page is <code>index.php</code> in your websites folder; delete it or replace it whenever you like.</p>
</main>
</body>
</html>
