const { describe, it, before } = require('node:test');
const assert = require('node:assert');
const request = require('supertest');

// Test with default environment variables (no env vars set)
describe('Default Environment Variables', () => {
  let app;

  before(() => {
    // Clear all environment variables to test defaults
    delete process.env.BROWSER_WORKER_URL;
    delete process.env.CONTROL_PLANE_URL;
    delete process.env.ALLOWED_TOOLS;
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

    // Mock fetch - should use default URLs
    global.fetch = async (url, options) => {
      // Verify default URLs are being used
      if (url.includes(':8082/tools/execute')) {
        // Default BROWSER_WORKER_URL
        return {
          ok: true,
          json: async () => ({ output: { title: 'Test' }, artifacts: [] })
        };
      }
      if (url.includes(':8080/runs/') && url.includes('/events')) {
        // Default CONTROL_PLANE_URL
        return { ok: true };
      }
      return { ok: true };
    };

    // Require server fresh with no env vars
    delete require.cache[require.resolve('./server')];
    const server = require('./server');
    app = server.app;
  });

  it('uses default ALLOWED_TOOLS when env not set', async () => {
    // browser.navigate should be in default allowlist
    const response = await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-default',
        invocation_id: 'inv-default',
        tool_name: 'browser.navigate',
        input: { url: 'https://example.com' }
      })
      .expect(200);

    assert.ok(response.body.output);
  });

  it('uses default CONTROL_PLANE_URL when env not set', async () => {
    const events = [];
    global.fetch = async (url, options) => {
      if (url.includes(':8080/runs/') && url.includes('/events')) {
        events.push(url);
        return { ok: true };
      }
      if (url.includes(':8082/tools/execute')) {
        return {
          ok: true,
          json: async () => ({ output: {}, artifacts: [] })
        };
      }
      return { ok: true };
    };

    await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-default-cp',
        invocation_id: 'inv-default-cp',
        tool_name: 'browser.navigate',
        input: {}
      })
      .expect(200);

    // Should have sent events to default control plane URL
    assert.ok(events.some(url => url.includes(':8080')));
  });

  it('uses default PROCESS_ALLOWLIST when env not set', async () => {
    const response = await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-default-proc',
        invocation_id: 'inv-default-proc',
        tool_name: 'process.exec',
        input: {
          command: 'node',
          args: ['--version'],
          cwd: '.',
        },
      })
      .expect(200);

    assert.strictEqual(response.body.status, 'completed');
    assert.ok(typeof response.body.output.stdout === 'string');
    assert.strictEqual(response.body.output.exit_code, 0);
  });
});
