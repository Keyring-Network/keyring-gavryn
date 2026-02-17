const { describe, it, before, after } = require("node:test");
const assert = require("node:assert");
const request = require("supertest");
const path = require("path");
const fs = require("fs/promises");

const originalEnv = { ...process.env };

describe("Tool Runner Editor and Process Tools", () => {
	let app;
	let emittedEvents = [];
	const runId = "run-editor";
	const workspaceRoot = path.resolve(__dirname, "..", "workspaces");
	const runRoot = path.join(workspaceRoot, runId);

  before(async () => {
    process.env.ALLOWED_TOOLS = "editor.list,editor.read,editor.write,editor.delete,editor.stat,process.exec,process.start,process.status,process.logs,process.stop,process.list";
    process.env.PROCESS_ALLOWLIST = "echo,node";
    process.env.CONTROL_PLANE_URL = "http://localhost:8080";
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

		global.fetch = async (_url, options = {}) => {
			if (options.body) {
				try {
					emittedEvents.push(JSON.parse(options.body));
				} catch (_err) {
					// ignore parse failures for non-JSON bodies
				}
			}
			return { ok: true, json: async () => ({}) };
		};

    await fs.rm(runRoot, { recursive: true, force: true });
    await fs.mkdir(runRoot, { recursive: true });

    delete require.cache[require.resolve("./server")];
    const server = require("./server");
    app = server.app;
  });

  after(async () => {
    await fs.rm(runRoot, { recursive: true, force: true });
    Object.keys(process.env).forEach((key) => delete process.env[key]);
    Object.assign(process.env, originalEnv);
    delete require.cache[require.resolve("./server")];
  });

	it("writes, reads, stats, and lists workspace files", async () => {
		emittedEvents = [];
		const writeResponse = await request(app)
			.post("/tools/execute")
			.send({
				run_id: runId,
        invocation_id: "inv-write",
        tool_name: "editor.write",
        input: { path: "notes.txt", content: "hello" },
      })
      .expect(200);

    assert.strictEqual(writeResponse.body.output.path, "notes.txt");

    const readResponse = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-read",
        tool_name: "editor.read",
        input: { path: "notes.txt" },
      })
      .expect(200);

    assert.strictEqual(readResponse.body.output.content, "hello");

    const statResponse = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-stat",
        tool_name: "editor.stat",
        input: { path: "notes.txt" },
      })
      .expect(200);

    assert.strictEqual(statResponse.body.output.type, "file");

    const listResponse = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-list",
        tool_name: "editor.list",
        input: { path: "." },
      })
      .expect(200);

		const names = listResponse.body.output.entries.map((entry) => entry.name);
		assert.ok(names.includes("notes.txt"));

		const workspaceEvent = emittedEvents.find((event) => event.type === "workspace.changed" && event.payload?.path === "notes.txt");
		assert.ok(workspaceEvent, "expected workspace.changed event for write");
		assert.strictEqual(workspaceEvent.payload.change, "added");
	});

	it("deletes workspace files", async () => {
		emittedEvents = [];
		await request(app)
			.post("/tools/execute")
			.send({
        run_id: runId,
        invocation_id: "inv-write-delete",
        tool_name: "editor.write",
        input: { path: "notes.txt", content: "bye" },
      })
      .expect(200);

    await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-delete",
        tool_name: "editor.delete",
        input: { path: "notes.txt" },
      })
      .expect(200);

    await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-list-after",
        tool_name: "editor.list",
        input: { path: "." },
      })
      .expect(200)
			.then((response) => {
				const names = response.body.output.entries.map((entry) => entry.name);
				assert.ok(!names.includes("notes.txt"));
			});

		const workspaceEvent = emittedEvents.find((event) => event.type === "workspace.changed" && event.payload?.path === "notes.txt" && event.payload.change === "removed");
		assert.ok(workspaceEvent, "expected workspace.changed event for delete");
	});

  it("executes allowlisted commands", async () => {
    const response = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-exec",
        tool_name: "process.exec",
        input: { command: "echo", args: ["hello"] },
      })
      .expect(200);

    assert.ok(response.body.output.stdout.includes("hello"));
  });

  it("manages long-running processes", async () => {
    const startResponse = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-proc-start",
        tool_name: "process.start",
        input: { command: "node", args: ["-e", "setInterval(()=>console.log('ready'),200)"] },
      })
      .expect(200);

    const processId = startResponse.body.output.process_id;
    assert.ok(processId);

    const statusResponse = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-proc-status",
        tool_name: "process.status",
        input: { process_id: processId },
      })
      .expect(200);

    assert.strictEqual(statusResponse.body.output.process_id, processId);
    assert.ok(statusResponse.body.output.status === "running" || statusResponse.body.output.status === "exited");

    const logsResponse = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-proc-logs",
        tool_name: "process.logs",
        input: { process_id: processId, tail: 50 },
      })
      .expect(200);

    assert.strictEqual(logsResponse.body.output.process_id, processId);
    assert.ok(Array.isArray(logsResponse.body.output.logs));

    const listResponse = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-proc-list",
        tool_name: "process.list",
        input: {},
      })
      .expect(200);

    assert.ok(Array.isArray(listResponse.body.output.processes));

    await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-proc-stop",
        tool_name: "process.stop",
        input: { process_id: processId },
      })
      .expect(200);
  });

  it("cleans up all run processes via cleanup endpoint", async () => {
    const startResponse = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-proc-start-cleanup",
        tool_name: "process.start",
        input: { command: "node", args: ["-e", "setInterval(()=>console.log('alive'),200)"] },
      })
      .expect(200);

    assert.ok(startResponse.body.output.process_id);

    const cleanupResponse = await request(app)
      .post(`/runs/${runId}/processes/cleanup`)
      .send({ force: true })
      .expect(200);

    assert.strictEqual(cleanupResponse.body.status, "completed");
    assert.ok(cleanupResponse.body.output.stopped >= 1);
  });

  it("blocks path traversal", async () => {
    const response = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-traverse",
        tool_name: "editor.read",
        input: { path: "../secret.txt" },
      })
      .expect(500);

    assert.ok(response.body.error.includes("path escapes"));
  });

  it("normalizes /context-prefixed paths", async () => {
    await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-context-write",
        tool_name: "editor.write",
        input: { path: "/context/src/index.ts", content: "export const ok = true;" },
      })
      .expect(200);

    const response = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-context-read",
        tool_name: "editor.read",
        input: { path: "src/index.ts" },
      })
      .expect(200);

    assert.strictEqual(response.body.output.content, "export const ok = true;");
  });

  it("normalizes absolute-style paths under run root", async () => {
    const response = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-abs-write",
        tool_name: "editor.write",
        input: { path: "/notes/absolute.txt", content: "hello absolute" },
      })
      .expect(200);

    assert.strictEqual(response.body.output.path, "notes/absolute.txt");
  });

  it("accepts file_path alias for editor.write", async () => {
    await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-file-path-write",
        tool_name: "editor.write",
        input: { file_path: "aliases/file-path.txt", content: "hello alias" },
      })
      .expect(200);

    const response = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-file-path-read",
        tool_name: "editor.read",
        input: { path: "aliases/file-path.txt" },
      })
      .expect(200);

    assert.strictEqual(response.body.output.content, "hello alias");
  });

  it("accepts filepath alias for editor.write", async () => {
    await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-filepath-write",
        tool_name: "editor.write",
        input: { filepath: "aliases/filepath.txt", content: "hello filepath" },
      })
      .expect(200);

    const response = await request(app)
      .post("/tools/execute")
      .send({
        run_id: runId,
        invocation_id: "inv-filepath-read",
        tool_name: "editor.read",
        input: { path: "aliases/filepath.txt" },
      })
      .expect(200);

    assert.strictEqual(response.body.output.content, "hello filepath");
  });
});
