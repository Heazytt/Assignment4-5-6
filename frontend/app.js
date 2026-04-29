// All API calls go through nginx (/api/...) which routes per-prefix to the right service.
const API = "/api";

let token = localStorage.getItem("token") || "";
let user = JSON.parse(localStorage.getItem("user") || "null");
let cart = []; // [{id, name, price, qty}]

function log(msg, ok = true) {
  const el = document.getElementById("log");
  const cls = ok ? "log-ok" : "log-bad";
  const ts = new Date().toLocaleTimeString();
  el.innerHTML = `<span class="${cls}">[${ts}] ${msg}</span>\n` + el.innerHTML;
}

function authHeaders() {
  return token ? { "Authorization": "Bearer " + token } : {};
}

async function api(path, opts = {}) {
  const res = await fetch(API + path, {
    ...opts,
    headers: {
      "Content-Type": "application/json",
      ...authHeaders(),
      ...(opts.headers || {}),
    },
  });
  const text = await res.text();
  let body;
  try { body = JSON.parse(text); } catch { body = text; }
  if (!res.ok) {
    const msg = (body && body.error) || text || res.statusText;
    throw new Error(`${res.status} ${msg}`);
  }
  return body;
}

function refreshUI() {
  if (token && user) {
    document.getElementById("authSection").hidden = true;
    document.getElementById("appSection").hidden = false;
    document.getElementById("userInfo").textContent = `${user.name} (id=${user.id})`;
    document.getElementById("logoutBtn").hidden = false;
    loadProducts();
    loadOrders();
  } else {
    document.getElementById("authSection").hidden = false;
    document.getElementById("appSection").hidden = true;
    document.getElementById("userInfo").textContent = "";
    document.getElementById("logoutBtn").hidden = true;
  }
}

async function register() {
  try {
    const data = await api("/auth/register", {
      method: "POST",
      body: JSON.stringify({
        email: document.getElementById("regEmail").value,
        name: document.getElementById("regName").value,
        password: document.getElementById("regPass").value,
      }),
    });
    token = data.token;
    user = data.user;
    localStorage.setItem("token", token);
    localStorage.setItem("user", JSON.stringify(user));
    log(`Registered as ${user.email}`);
    refreshUI();
  } catch (e) { log(e.message, false); }
}

async function login() {
  try {
    const data = await api("/auth/login", {
      method: "POST",
      body: JSON.stringify({
        email: document.getElementById("loginEmail").value,
        password: document.getElementById("loginPass").value,
      }),
    });
    token = data.token;
    user = data.user;
    localStorage.setItem("token", token);
    localStorage.setItem("user", JSON.stringify(user));
    log(`Logged in as ${user.email}`);
    refreshUI();
  } catch (e) { log(e.message, false); }
}

document.getElementById("logoutBtn").onclick = () => {
  token = ""; user = null; cart = [];
  localStorage.removeItem("token"); localStorage.removeItem("user");
  log("Logged out");
  refreshUI();
};

async function loadProducts() {
  try {
    const items = await api("/products");
    const el = document.getElementById("products");
    el.innerHTML = "";
    items.forEach(p => {
      const div = document.createElement("div");
      div.className = "product";
      div.innerHTML = `
        <div><strong>${p.name}</strong></div>
        <div class="price">$${p.price.toFixed(2)}</div>
        <div class="stock">stock: ${p.stock}</div>
        <div>${p.description}</div>
        <button>Add to cart</button>`;
      div.querySelector("button").onclick = () => addToCart(p);
      el.appendChild(div);
    });
    log(`Loaded ${items.length} products`);
  } catch (e) { log(e.message, false); }
}

function addToCart(p) {
  const exist = cart.find(i => i.id === p.id);
  if (exist) exist.qty++;
  else cart.push({ id: p.id, name: p.name, price: p.price, qty: 1 });
  renderCart();
}

function renderCart() {
  const el = document.getElementById("cart");
  if (!cart.length) { el.innerHTML = "<i>empty</i>"; return; }
  let total = 0;
  el.innerHTML = cart.map(i => {
    total += i.qty * i.price;
    return `<div>${i.name} × ${i.qty} = $${(i.qty * i.price).toFixed(2)}</div>`;
  }).join("") + `<hr/><strong>Total: $${total.toFixed(2)}</strong>`;
}

async function checkout() {
  if (!cart.length) return log("Cart is empty", false);
  try {
    const o = await api("/orders", {
      method: "POST",
      body: JSON.stringify({
        items: cart.map(i => ({ product_id: i.id, quantity: i.qty })),
      }),
    });
    log(`Order #${o.id} created, total $${o.total.toFixed(2)}`);
    cart = []; renderCart();
    loadProducts(); loadOrders();
  } catch (e) {
    log("Checkout failed: " + e.message, false);
  }
}

async function loadOrders() {
  try {
    const orders = await api("/orders");
    const el = document.getElementById("orders");
    if (!orders.length) { el.innerHTML = "<i>no orders yet</i>"; return; }
    el.innerHTML = orders.map(o =>
      `<div>#${o.id} — $${o.total.toFixed(2)} — <em>${o.status}</em> — ${new Date(o.created_at).toLocaleString()}</div>`
    ).join("");
  } catch (e) { log(e.message, false); }
}

async function sendChat() {
  const to = parseInt(document.getElementById("chatTo").value, 10);
  const msg = document.getElementById("chatMsg").value;
  if (!to || !msg) return log("recipient and message required", false);
  try {
    await api("/chat/send", {
      method: "POST",
      body: JSON.stringify({ receiver_id: to, content: msg }),
    });
    document.getElementById("chatMsg").value = "";
    log(`Sent message to user ${to}`);
  } catch (e) { log(e.message, false); }
}

async function loadInbox() {
  try {
    const msgs = await api("/chat/inbox");
    const el = document.getElementById("chatBox");
    if (!msgs.length) { el.innerHTML = "<i>inbox empty</i>"; return; }
    el.innerHTML = msgs.map(m =>
      `<div class="msg"><div class="meta">from user ${m.sender_id} · ${new Date(m.created_at).toLocaleTimeString()}</div>${m.content}</div>`
    ).join("");
  } catch (e) { log(e.message, false); }
}

// Poll inbox every 5s when logged in.
setInterval(() => { if (token) loadInbox().catch(() => {}); }, 5000);

refreshUI();
