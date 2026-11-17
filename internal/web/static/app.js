"use strict";

// Demo users from migration 00002_seed.sql. The server trusts X-User-ID (no auth, demo only).
const USERS = [
  { id: "11111111-1111-1111-1111-111111111111", name: "Petya" },
  { id: "22222222-2222-2222-2222-222222222222", name: "Masha" },
];

const state = {
  me: USERS[0],
  accounts: [],        // my accounts
  selectedId: null,    // account shown in the statement
  lastTransfer: null,  // { user, key, body } for "Retry same key"
  logNo: 0,
  keyFirstSeen: new Map(), // Idempotency-Key → log number of its first request
};

const $ = (id) => document.getElementById(id);

// ---------- helpers ----------

// crypto.randomUUID works only on https or localhost. On the Raspberry Pi over plain http
// it is missing, so build a v4 UUID from getRandomValues, which works everywhere.
function newKey() {
  if (crypto.randomUUID) return crypto.randomUUID();
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

// The API works in cents (10000 = 100.00 €); the page shows and takes euros.
function money(cents, currency = "EUR") {
  return new Intl.NumberFormat("en-IE", { style: "currency", currency }).format(cents / 100);
}
function toCents(input) {
  const value = Number(input.value);
  return Number.isFinite(value) ? Math.round(value * 100) : 0;
}

function other(user) {
  return USERS.find((u) => u.id !== user.id);
}

// ---------- API ----------

// api sends one request, writes it to the log and returns { status, data }.
async function api(method, path, { user = state.me, key, body } = {}) {
  const headers = { "X-User-ID": user.id };
  if (key) headers["Idempotency-Key"] = key;
  if (body !== undefined) headers["Content-Type"] = "application/json";

  let status = 0;
  let data = null;
  try {
    const res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
    status = res.status;
    const text = await res.text();
    data = text ? JSON.parse(text) : null;
  } catch (err) {
    data = { error: String(err) };
  }
  log({ method, path, user, key, body, status, data });
  return { status, data };
}

// ---------- log ----------

function log({ method, path, user, key, body, status, data }) {
  state.logNo += 1;
  const li = document.createElement("li");
  li.className = method === "GET" ? "entry read" : "entry";

  const line = document.createElement("div");
  line.className = "line";
  const st = document.createElement("span");
  st.className = `status s${String(status)[0] || 0}`;
  st.textContent = status || "ERR";
  const req = document.createElement("span");
  req.className = "req";
  req.textContent = `${method} ${path}`;
  line.append(st, req);

  const meta = document.createElement("div");
  meta.className = "meta";
  let keyNote = "";
  if (key) {
    const first = state.keyFirstSeen.get(key);
    keyNote = ` · key ${key.slice(0, 8)}…` + (first ? ` · same key as #${first}` : "");
    if (!first) state.keyFirstSeen.set(key, state.logNo);
  }
  meta.textContent = `#${state.logNo} · as ${user.name}${keyNote}`;

  li.append(line, meta);
  if (body !== undefined) li.append(pre("→ " + JSON.stringify(body)));
  if (data !== null) li.append(pre("← " + JSON.stringify(data)));

  $("log").prepend(li);
}

function pre(text) {
  const el = document.createElement("pre");
  el.textContent = text; // textContent, never innerHTML: server data is not trusted markup
  return el;
}

// ---------- accounts tab ----------

async function loadAccounts() {
  const { status, data } = await api("GET", "/accounts");
  state.accounts = status === 200 ? data.accounts : [];
  if (!state.accounts.some((a) => a.id === state.selectedId)) {
    state.selectedId = state.accounts[0]?.id ?? null;
  }
  renderAccounts();
  await loadStatement();
  await fillTransferForm();
}

function renderAccounts() {
  const box = $("accounts");
  box.replaceChildren();
  for (const acc of state.accounts) {
    const b = document.createElement("button");
    b.className = "account";
    b.setAttribute("aria-pressed", String(acc.id === state.selectedId));
    const cur = document.createElement("div");
    cur.className = "cur";
    cur.textContent = `${acc.currency} · ${acc.id.slice(0, 8)}…`;
    const bal = document.createElement("div");
    bal.className = "bal";
    bal.textContent = money(acc.balance, acc.currency);
    b.append(cur, bal);
    b.onclick = () => {
      state.selectedId = acc.id;
      renderAccounts();
      loadStatement();
    };
    box.append(b);
  }
}

async function loadStatement() {
  const body = $("statement");
  body.replaceChildren();
  const acc = state.accounts.find((a) => a.id === state.selectedId);
  $("statement-for").textContent = acc ? `· ${acc.currency}` : "";
  if (!acc) {
    $("statement-empty").hidden = false;
    return;
  }

  const { status, data } = await api("GET", `/accounts/${acc.id}/postings`);
  const rows = status === 200 ? data.postings : [];
  $("statement-empty").hidden = rows.length > 0;
  for (const p of rows) {
    const tr = document.createElement("tr");
    const amount = cell(money(p.amount, acc.currency), "num " + (p.amount > 0 ? "plus" : "minus"));
    if (p.amount > 0) amount.textContent = "+" + amount.textContent;
    tr.append(
      cell(String(p.operation_id)),
      cell(p.type),
      amount,
      cell(new Date(p.created_at).toLocaleTimeString()),
    );
    body.append(tr);
  }
}

function cell(text, cls) {
  const td = document.createElement("td");
  td.textContent = text;
  if (cls) td.className = cls;
  return td;
}

async function deposit(event) {
  event.preventDefault();
  if (!state.selectedId) return;
  await api("POST", `/accounts/${state.selectedId}/deposits`, {
    key: newKey(),
    body: { amount: toCents($("deposit-amount")) },
  });
  await loadAccounts();
}

// ---------- transfer tab ----------

// The recipient's accounts are read with the recipient's X-User-ID: the server shows
// a user only their own accounts.
async function fillTransferForm() {
  fillSelect($("from"), state.accounts, state.me);
  const peer = other(state.me);
  const { status, data } = await api("GET", "/accounts", { user: peer });
  fillSelect($("to"), status === 200 ? data.accounts : [], peer);
}

function fillSelect(select, accounts, owner) {
  const keep = select.value;
  select.replaceChildren();
  for (const acc of accounts) {
    const opt = document.createElement("option");
    opt.value = acc.id;
    opt.textContent = owner.id === state.me.id
      ? `${acc.currency} · ${money(acc.balance, acc.currency)}`
      : `${owner.name} · ${acc.currency}`;
    select.append(opt);
  }
  if ([...select.options].some((o) => o.value === keep)) select.value = keep;
}

function transferBody() {
  return {
    from_account_id: $("from").value,
    to_account_id: $("to").value,
    amount: toCents($("transfer-amount")),
  };
}

async function send() {
  const t = { user: state.me, key: newKey(), body: transferBody() };
  rememberTransfer(t);
  await api("POST", "/transfers", t);
  await loadAccounts();
}

async function sendTwice() {
  const body = transferBody();
  const a = { user: state.me, key: newKey(), body };
  const b = { user: state.me, key: newKey(), body };
  const [ra, rb] = await Promise.all([api("POST", "/transfers", a), api("POST", "/transfers", b)]);
  // For "Retry same key" keep the transfer that went through: its replay shows the same operation_id.
  rememberTransfer(ra.status === 201 ? a : rb.status === 201 ? b : b);
  await loadAccounts();
}

async function retry() {
  if (!state.lastTransfer) return;
  await api("POST", "/transfers", state.lastTransfer);
  await loadAccounts();
}

function rememberTransfer(t) {
  state.lastTransfer = t;
  $("retry").disabled = false;
}

// ---------- tabs and user switch ----------

function showTab(name) {
  for (const t of ["accounts", "transfer"]) {
    $(`tab-${t}`).setAttribute("aria-selected", String(t === name));
    $(`view-${t}`).hidden = t !== name;
  }
}

function renderWho() {
  const box = $("who");
  box.replaceChildren();
  for (const u of USERS) {
    const b = document.createElement("button");
    b.textContent = u.name;
    b.setAttribute("aria-pressed", String(u.id === state.me.id));
    b.onclick = () => {
      state.me = u;
      state.selectedId = null;
      state.lastTransfer = null;
      $("retry").disabled = true;
      renderWho();
      loadAccounts();
    };
    box.append(b);
  }
}

$("tab-accounts").onclick = () => showTab("accounts");
$("tab-transfer").onclick = () => showTab("transfer");
$("deposit-form").onsubmit = deposit;
$("send").onclick = send;
$("send-twice").onclick = sendTwice;
$("retry").onclick = retry;
$("clear-log").onclick = () => $("log").replaceChildren();
$("show-reads").onchange = (e) => $("log").classList.toggle("hide-reads", !e.target.checked);

renderWho();
loadAccounts();
