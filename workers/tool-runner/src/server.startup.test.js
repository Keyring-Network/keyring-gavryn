const { describe, it } = require('node:test');
const assert = require('node:assert');
const request = require('supertest');
const { spawn } = require('child_process');
const path = require('path');

// Test server startup when run as main module
describe('Server Startup', () => {
  it('starts server when run as main module', async () => {
    const serverPath = path.join(__dirname, 'server.js');

    // Spawn server as child process
    const child = spawn('node', [serverPath], {
      env: {
        ...process.env,
        TOOL_RUNNER_PORT: '9999',
        BROWSER_WORKER_URL: 'http://localhost:3001',
        CONTROL_PLANE_URL: 'http://localhost:8080',
        ALLOWED_TOOLS: 'browser.navigate'
      },
      stdio: 'pipe'
    });

    let output = '';
    child.stdout.on('data', (data) => {
      output += data.toString();
    });

    // Wait for server to start
    await new Promise((resolve) => setTimeout(resolve, 500));

    // Verify server started
    assert.ok(output.includes('tool runner listening on 9999'));

    // Clean up
    child.kill();

    // Wait for process to exit
    await new Promise((resolve) => {
      child.on('exit', resolve);
    });
  });
});
