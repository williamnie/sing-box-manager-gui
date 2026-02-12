const MIN_NODE_MAJOR = 18;
const currentVersion = process.versions.node;
const majorVersion = Number.parseInt(currentVersion.split('.')[0], 10);

if (Number.isNaN(majorVersion) || majorVersion < MIN_NODE_MAJOR) {
  console.error(`\n[sing-box-manager] Node.js version is too low: ${currentVersion}`);
  console.error(`[sing-box-manager] Required: Node.js >= ${MIN_NODE_MAJOR}.0.0\n`);
  process.exit(1);
}
