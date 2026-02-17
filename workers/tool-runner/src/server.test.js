const { describe, it, before, after } = require('node:test');
const assert = require('node:assert');
const request = require('supertest');

// Mock fetch before requiring the server
global.fetch = async () => ({ ok: true });

// Store original env
const originalEnv = { ...process.env };

describe('Tool Runner Server', () => {
  let app;
  let fetchCalls = [];

  before(() => {
    // Set up environment
    process.env.BROWSER_WORKER_URL = 'http://localhost:3001';
    process.env.CONTROL_PLANE_URL = 'http://localhost:8080';
    process.env.ALLOWED_TOOLS = 'browser.navigate,browser.snapshot,browser.extract';
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

    // Clear module cache to get fresh instance
    delete require.cache[require.resolve('./server')];

    // Mock fetch to capture calls
    fetchCalls = [];
    global.fetch = async (url, options) => {
      fetchCalls.push({ url, options });
      return { ok: true };
    };

    // Require server after env setup
    const server = require('./server');
    app = server.app;
    app = server.app;
  });

  after(() => {
    // Restore original env
    Object.keys(process.env).forEach(key => { delete process.env[key]; });
    Object.assign(process.env, originalEnv);

    // Clear module cache
    delete require.cache[require.resolve('./server')];
  });

  describe('Health Endpoint', () => {
    it('GET /health returns 200', async () => {
      const response = await request(app)
        .get('/health')
        .expect(200);

      assert.deepStrictEqual(response.body, { status: 'ok' });
    });

    it('GET /tools/capabilities returns tool and browser status', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes(':3001/ready') || url.includes(':3001/health')) {
          return { ok: true };
        }
        return { ok: true };
      };

      const response = await request(app)
        .get('/tools/capabilities')
        .expect(200);

      assert.strictEqual(response.body.status, 'ok');
      assert.ok(Array.isArray(response.body.tools));
      assert.strictEqual(response.body.browser.enabled, true);
      assert.strictEqual(response.body.browser.healthy, true);
    });

    it('GET /ready returns 200 when browser worker healthy', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes(':3001/ready') || url.includes(':3001/health')) {
          return { ok: true };
        }
        return { ok: true };
      };

      const response = await request(app)
        .get('/ready')
        .expect(200);

      assert.strictEqual(response.body.status, 'ok');
      assert.strictEqual(response.body.subsystems.browser_worker.status, 'ok');
    });

    it('GET /ready returns 503 when browser worker unhealthy and required', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes(':3001/ready') || url.includes(':3001/health')) {
          return { ok: false };
        }
        return { ok: true };
      };

      const response = await request(app)
        .get('/ready')
        .expect(503);

      assert.strictEqual(response.body.status, 'degraded');
      assert.strictEqual(response.body.subsystems.browser_worker.status, 'error');
    });
  });

  describe('Execute Endpoint - Validation', () => {
    it('POST /tools/execute without run_id returns 400', async () => {
      const response = await request(app)
        .post('/tools/execute')
        .send({
          invocation_id: 'inv-123',
          tool_name: 'browser.navigate'
        })
        .expect(400);

      assert.strictEqual(response.body.error, 'run_id, idempotency_key, and tool_name are required');
    });

    it('POST /tools/execute without invocation_id returns 400', async () => {
      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-123',
          tool_name: 'browser.navigate'
        })
        .expect(400);

      assert.strictEqual(response.body.error, 'run_id, idempotency_key, and tool_name are required');
    });

    it('POST /tools/execute without tool_name returns 400', async () => {
      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-123',
          invocation_id: 'inv-123'
        })
        .expect(400);

      assert.strictEqual(response.body.error, 'run_id, idempotency_key, and tool_name are required');
    });

    it('POST /tools/execute with empty body returns 400', async () => {
      const response = await request(app)
        .post('/tools/execute')
        .send({})
        .expect(400);

      assert.strictEqual(response.body.error, 'run_id, idempotency_key, and tool_name are required');
    });
  });

  describe('Execute Endpoint - Allowlist', () => {
    it('tool not in allowlist returns 403', async () => {
      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-123',
          invocation_id: 'inv-123',
          tool_name: 'unauthorized.tool'
        })
        .expect(403);

      assert.strictEqual(response.body.error, 'tool not allowlisted: unauthorized.tool');
    });

    it('browser.navigate in allowlist proceeds', async () => {
      // Mock successful browser response
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: true,
            json: async () => ({
              output: { title: 'Test Page' },
              artifacts: [{ type: 'screenshot', data: 'base64data' }]
            })
          };
        }
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-123',
          invocation_id: 'inv-123',
          tool_name: 'browser.navigate',
          input: { url: 'https://example.com' }
        })
        .expect(200);

      assert.strictEqual(response.body.output.title, 'Test Page');
    });

    it('browser.snapshot in allowlist proceeds', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: true,
            json: async () => ({
              output: { html: '<html></html>' },
              artifacts: []
            })
          };
        }
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-123',
          invocation_id: 'inv-123',
          tool_name: 'browser.snapshot',
          input: {}
        })
        .expect(200);

      assert.strictEqual(response.body.output.html, '<html></html>');
    });

    it('rejects browser.extract_text alias (canonical tool names only)', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-123',
          invocation_id: 'inv-extract-alias',
          tool_name: 'browser.extract_text',
          input: { selector: 'body' }
        })
        .expect(403);

      assert.strictEqual(response.body.reason_code, 'tool_not_allowlisted');
    });
  });

  describe('Execute Endpoint - Browser Proxy', () => {
    it('successful browser tool execution returns 200', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: true,
            json: async () => ({
              output: { title: 'Success', url: 'https://example.com' },
              artifacts: [{ type: 'screenshot', data: 'base64' }]
            })
          };
        }
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-456',
          invocation_id: 'inv-456',
          tool_name: 'browser.navigate',
          input: { url: 'https://test.com' },
          timeout_ms: 30000
        })
        .expect(200);

      assert.deepStrictEqual(response.body.output, { title: 'Success', url: 'https://example.com' });
      assert.strictEqual(response.body.artifacts.length, 1);
    });

    it('browser worker returns error - 500', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: false,
            status: 500,
            text: async () => 'Browser crashed'
          };
        }
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-789',
          invocation_id: 'inv-789',
          tool_name: 'browser.navigate',
          input: { url: 'https://error.com' }
        })
        .expect(500);

      assert.strictEqual(response.body.status, 'failed');
      assert.ok(response.body.error.includes('Browser crashed'));
    });

    it('propagates browser worker reason_code and error message', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: false,
            status: 500,
            text: async () => JSON.stringify({
              status: 'failed',
              error: 'User tab mode could not connect to http://127.0.0.1:9222',
              reason_code: 'user_tab_mode_unavailable'
            })
          };
        }
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-user-tab',
          invocation_id: 'inv-user-tab',
          tool_name: 'browser.navigate',
          input: { url: 'https://example.com' }
        })
        .expect(500);

      assert.strictEqual(response.body.status, 'failed');
      assert.strictEqual(response.body.reason_code, 'user_tab_mode_unavailable');
      assert.ok(response.body.error.includes('could not connect'));
    });

    it('browser worker returns empty error - 500', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: false,
            status: 500,
            text: async () => ''
          };
        }
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-empty',
          invocation_id: 'inv-empty',
          tool_name: 'browser.navigate',
          input: {}
        })
        .expect(500);

      assert.strictEqual(response.body.status, 'failed');
      assert.strictEqual(response.body.error, 'browser worker failed');
    });

    it('browser worker unreachable - 500', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/tools/execute') && url.includes('3001')) {
          throw new Error('Connection refused');
        }
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-net',
          invocation_id: 'inv-net',
          tool_name: 'browser.navigate',
          input: {}
        })
        .expect(500);

      assert.strictEqual(response.body.status, 'failed');
      assert.ok(response.body.error.includes('Connection refused'));
    });

    it('dedupes repeated invocation_id and returns cached result', async () => {
      let browserExecuteCalls = 0;
      const emittedEventTypes = [];
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/runs/') && url.includes('/events')) {
          const body = JSON.parse(options.body);
          emittedEventTypes.push(body.type);
          return { ok: true };
        }
        if (url.includes('/tools/execute') && url.includes('3001')) {
          browserExecuteCalls += 1;
          return {
            ok: true,
            json: async () => ({
              output: { title: 'Cached Title', url: 'https://cached.example' },
              artifacts: [{ type: 'screenshot', data: 'base64' }]
            })
          };
        }
        return { ok: true };
      };

      const payload = {
        run_id: 'run-dedupe',
        invocation_id: 'inv-dedupe',
        tool_name: 'browser.navigate',
        input: { url: 'https://dedupe.test' }
      };

      const first = await request(app)
        .post('/tools/execute')
        .send(payload)
        .expect(200);
      assert.strictEqual(first.body.deduped, undefined);
      assert.strictEqual(first.body.output.title, 'Cached Title');

      const second = await request(app)
        .post('/tools/execute')
        .send(payload)
        .expect(200);
      assert.strictEqual(second.body.deduped, true);
      assert.strictEqual(second.body.output.title, 'Cached Title');

      assert.strictEqual(browserExecuteCalls, 1, 'browser worker should be called only once');
      assert.ok(emittedEventTypes.includes('tool.deduped'), 'should emit tool.deduped event on cached replay');
    });
  });

  describe('Execute Endpoint - Event Emission', () => {
    it('emits started event on execution', async () => {
      const events = [];
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/runs/') && url.includes('/events')) {
          const body = JSON.parse(options.body);
          events.push({ url, type: body.type });
        }
        if (url.includes('/tools/execute') && url.includes('3001')) {
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
          run_id: 'run-events',
          invocation_id: 'inv-events',
          tool_name: 'browser.navigate',
          input: { url: 'https://test.com' }
        })
        .expect(200);

      const startedEvent = events.find(e => e.type === 'tool.started');
      assert.ok(startedEvent, 'Should emit tool.started event');
    });

    it('emits output event with result', async () => {
      const events = [];
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/runs/') && url.includes('/events')) {
          const body = JSON.parse(options.body);
          events.push(body);
        }
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: true,
            json: async () => ({
              output: { title: 'Test' },
              artifacts: [{ type: 'screenshot' }]
            })
          };
        }
        return { ok: true };
      };

      await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-output',
          invocation_id: 'inv-output',
          tool_name: 'browser.navigate',
          input: {}
        })
        .expect(200);

      const outputEvent = events.find(e => e.type === 'tool.completed');
      assert.ok(outputEvent, 'Should emit tool.completed event');
      assert.deepStrictEqual(outputEvent.payload.output, { title: 'Test' });
      assert.deepStrictEqual(outputEvent.payload.artifacts, [{ type: 'screenshot' }]);
    });

    it('emits failed event on error', async () => {
      const events = [];
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/runs/') && url.includes('/events')) {
          const body = JSON.parse(options.body);
          events.push(body);
        }
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: false,
            status: 500,
            text: async () => 'Execution failed'
          };
        }
        return { ok: true };
      };

      await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-fail',
          invocation_id: 'inv-fail',
          tool_name: 'browser.navigate',
          input: {}
        })
        .expect(500);

      const failedEvent = events.find(e => e.type === 'tool.failed');
      assert.ok(failedEvent, 'Should emit tool.failed event');
      assert.ok(failedEvent.payload.error.includes('Execution failed'));
    });

    it('handles emitEvent failure gracefully', async () => {
      global.fetch = async (url, options) => {
        fetchCalls.push({ url, options });
        if (url.includes('/runs/') && url.includes('/events')) {
          throw new Error('Control plane unreachable');
        }
        if (url.includes('/tools/execute') && url.includes('3001')) {
          return {
            ok: true,
            json: async () => ({ output: {}, artifacts: [] })
          };
        }
        return { ok: true };
      };

      // Should not throw even if event emission fails
      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-emit-fail',
          invocation_id: 'inv-emit-fail',
          tool_name: 'browser.navigate',
          input: {}
        })
        .expect(200);

      assert.ok(response.body);
    });
  });

  describe('Helper Functions - resolvePort', () => {
    it('resolvePort parses valid PORT env', async () => {
      // Need to test with a fresh module
      delete require.cache[require.resolve('./server')];
      process.env.PORT = '9000';
      delete process.env.TOOL_RUNNER_PORT;

      // Capture console.warn calls
      const warnings = [];
      const originalWarn = console.warn;
      console.warn = (...args) => warnings.push(args.join(' '));

      require('./server');

      console.warn = originalWarn;
      delete process.env.PORT;

      // No warning should be emitted for valid port
      assert.strictEqual(warnings.length, 0);
    });

    it('resolvePort defaults to 8081 when no env set', async () => {
      delete require.cache[require.resolve('./server')];
      delete process.env.PORT;
      delete process.env.TOOL_RUNNER_PORT;

      const warnings = [];
      const originalWarn = console.warn;
      console.warn = (...args) => warnings.push(args.join(' '));

      require('./server');

      console.warn = originalWarn;

      // No warning for undefined (just uses default)
      assert.strictEqual(warnings.filter(w => w.includes('8081')).length, 0);
    });

    it('resolvePort warns on invalid port string', async () => {
      delete require.cache[require.resolve('./server')];
      process.env.TOOL_RUNNER_PORT = 'invalid';

      const warnings = [];
      const originalWarn = console.warn;
      console.warn = (...args) => warnings.push(args.join(' '));

      require('./server');

      console.warn = originalWarn;
      delete process.env.TOOL_RUNNER_PORT;

      assert.ok(warnings.some(w => w.includes('Invalid') && w.includes('8081')));
    });

    it('resolvePort warns on negative port', async () => {
      delete require.cache[require.resolve('./server')];
      process.env.TOOL_RUNNER_PORT = '-1';

      const warnings = [];
      const originalWarn = console.warn;
      console.warn = (...args) => warnings.push(args.join(' '));

      require('./server');

      console.warn = originalWarn;
      delete process.env.TOOL_RUNNER_PORT;

      assert.ok(warnings.some(w => w.includes('Invalid') && w.includes('8081')));
    });

    it('resolvePort warns on port too high', async () => {
      delete require.cache[require.resolve('./server')];
      process.env.TOOL_RUNNER_PORT = '70000';

      const warnings = [];
      const originalWarn = console.warn;
      console.warn = (...args) => warnings.push(args.join(' '));

      require('./server');

      console.warn = originalWarn;
      delete process.env.TOOL_RUNNER_PORT;

      assert.ok(warnings.some(w => w.includes('Invalid') && w.includes('8081')));
    });

    it('resolvePort accepts TOOL_RUNNER_PORT over PORT', async () => {
      delete require.cache[require.resolve('./server')];
      process.env.TOOL_RUNNER_PORT = '7000';
      process.env.PORT = '8000';

      const warnings = [];
      const originalWarn = console.warn;
      console.warn = (...args) => warnings.push(args.join(' '));

      require('./server');

      console.warn = originalWarn;
      delete process.env.TOOL_RUNNER_PORT;
      delete process.env.PORT;

      // No warnings for valid port
      assert.strictEqual(warnings.length, 0);
    });
  });

  describe('Helper Functions - emitEvent', () => {
    it('emitEvent sends to control plane', async () => {
      const calls = [];
      global.fetch = async (url, options) => {
        calls.push({ url, options });
        return { ok: true };
      };

      // Trigger an execution to cause event emission
      global.fetch = async (url, options) => {
        calls.push({ url, options });
        if (url.includes('/tools/execute') && url.includes('3001')) {
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
          run_id: 'run-emit',
          invocation_id: 'inv-emit',
          tool_name: 'browser.navigate',
          input: {}
        })
        .expect(200);

      // Check that events were sent to control plane
      const eventCalls = calls.filter(c => c.url.includes('/runs/') && c.url.includes('/events'));
      assert.ok(eventCalls.length > 0, 'Should send events to control plane');

      // Verify event structure
      const startedCall = eventCalls.find(c => {
        const body = JSON.parse(c.options.body);
        return body.type === 'tool.started';
      });
      assert.ok(startedCall, 'Should have tool.started event');

      const body = JSON.parse(startedCall.options.body);
      assert.strictEqual(body.source, 'tool_runner');
      assert.ok(body.timestamp);
      assert.strictEqual(body.payload.tool_invocation_id, 'inv-emit');
      assert.strictEqual(body.payload.tool_name, 'browser.navigate');
    });

    it('emitEvent handles network errors', async () => {
      const errors = [];
      const originalError = console.error;
      console.error = (...args) => errors.push(args.join(' '));

      global.fetch = async (url, options) => {
        if (url.includes('/runs/') && url.includes('/events')) {
          throw new Error('Network error');
        }
        if (url.includes('/tools/execute') && url.includes('3001')) {
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
          run_id: 'run-emit-err',
          invocation_id: 'inv-emit-err',
          tool_name: 'browser.navigate',
          input: {}
        })
        .expect(200);

      console.error = originalError;

      assert.ok(errors.some(e => e.includes('failed to emit event')));
    });
  });

  describe('Edge Cases', () => {
    it('handles error with non-string message', async () => {
      global.fetch = async (url, options) => {
        if (url.includes('/tools/execute') && url.includes('3001')) {
          // Throw something that's not an Error object
          throw { weird: 'error' };
        }
        return { ok: true };
      };

      const response = await request(app)
        .post('/tools/execute')
        .send({
          run_id: 'run-weird',
          invocation_id: 'inv-weird',
          tool_name: 'browser.navigate',
          input: {}
        })
        .expect(500);

      assert.strictEqual(response.body.status, 'failed');
      // Should convert to string
      assert.ok(response.body.error);
    });
  });
});
