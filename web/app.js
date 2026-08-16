// 页面所有可变状态集中在一个对象里,方便理清"数据从哪来、去哪了"。
const state = {
  points: [],      // 用户加的地点 [{name, lat, lng}],第 0 个当起点
  markers: [],     // 地图上的标记对象,和 points 一一对应
  polyline: null,  // 路线折线
  map: null,
  // key 存在浏览器 localStorage:这是使用者自己的 key,不进代码、不进仓库(开源要求)
  mapKey: localStorage.getItem('amap_key') || '',
};

// ---------- 小工具 ----------
const $ = (id) => document.getElementById(id);

// ---------- 地图:加载 + 点选 ----------
// 高德 JS API 通过 <script> 动态加载,key 塞在 src 里。
// 换 key 时我们直接整页刷新(location.reload()),省掉"卸载旧脚本再重载"的麻烦。
function initMap() {
  state.map = new AMap.Map('map', { zoom: 11, center: [113.32, 23.13] }); // 默认广州
  $('mapHint').classList.add('hidden');
  state.map.on('click', (e) => {
    // 点地图 = 加一个点;第 0 个自动当起点
    addPoint(`地点${state.points.length + 1}`, e.lnglat.lat, e.lnglat.lng);
  });
  redrawMarkers();
}

function loadMap() {
  if (!state.mapKey) return; // 没 key,保持提示,用手动输入
  if (window.AMap) { initMap(); return; }
  const script = document.createElement('script');
  script.src = `https://webapi.amap.com/maps?v=2.0&key=${state.mapKey}`;
  script.onload = initMap;
  script.onerror = () => {
    $('mapHint').textContent = 'key 无效或未加入域名白名单,改用手动输入。';
  };
  document.head.appendChild(script);
}

// ---------- 点的新增 / 删除 ----------
function addPoint(name, lat, lng) {
  state.points.push({ name, lat, lng });
  if (state.map) {
    const marker = new AMap.Marker({ position: [lng, lat], title: name });
    state.map.add(marker);
    state.markers.push(marker);
  }
  renderList();
}

function removePoint(i) {
  state.points.splice(i, 1);
  if (state.map && state.markers[i]) state.map.remove(state.markers[i]);
  state.markers.splice(i, 1);
  renderList();
}

// 把地图上已有的点重新描一遍(比如:没有 key 时手动加的点,填 key 后要补上标记)
function redrawMarkers() {
  state.markers.forEach((m) => state.map.remove(m));
  state.markers = [];
  state.points.forEach((p) => addPoint(p.name, p.lat, p.lng));
}

// 把地点列表画到左侧面板(纯 DOM 操作,不碰地图)
function renderList() {
  $('pointList').innerHTML = '';
  state.points.forEach((p, i) => {
    const li = document.createElement('li');
    li.innerHTML = `<span class="badge">${i === 0 ? '起点' : '第' + i + '站'}</span>
      <span>${p.name}</span>
      <span class="coords">${p.lat.toFixed(4)}, ${p.lng.toFixed(4)}</span>
      <button onclick="removePoint(${i})">✕</button>`;
    $('pointList').appendChild(li);
  });
}

// ---------- 规划:调后端 /plan(同源,相对路径,前端不需要知道端口) ----------
async function plan() {
  if (state.points.length < 2) { alert('至少需要一个起点 + 一个目的地'); return; }
  const res = await fetch('/plan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ origin: state.points[0], destinations: state.points.slice(1) }),
  });
  const data = await res.json();
  if (!res.ok) { alert(data.error || '规划失败'); return; }

  $('result').innerHTML = '顺序:' +
    data.order.map((n, i) => `<b>${i + 1}. ${n}</b>`).join(' → ') +
    `<br>总距离:<b>${data.total_km.toFixed(1)} km</b>`;
  drawRoute(data.order);
}

// 按后端返回的顺序,把坐标连成折线画在地图上
function drawRoute(order) {
  if (!state.map) return;
  const path = order.map((name) => {
    const p = state.points.find((pt) => pt.name === name);
    return [p.lng, p.lat];
  });
  if (state.polyline) state.map.remove(state.polyline);
  state.polyline = new AMap.Polyline({ path, strokeColor: '#1a73e8', strokeWeight: 5, lineJoin: 'round' });
  state.map.add(state.polyline);
  state.map.setFitView(); // 缩放到能装下所有标记
}

// ---------- 设置:换 key ----------
$('settingsBtn').onclick = () => {
  $('keyInput').value = state.mapKey;
  $('settingsOverlay').classList.remove('hidden');
};
$('closeSettingsBtn').onclick = () => $('settingsOverlay').classList.add('hidden');
$('saveKeyBtn').onclick = () => {
  const key = $('keyInput').value.trim();
  if (!key) { alert('key 不能为空'); return; }
  localStorage.setItem('amap_key', key);
  location.reload(); // 换 key 要重载地图,整页刷新最省事
};

// ---------- 手动添加 ----------
$('addBtn').onclick = () => {
  const name = $('nameInput').value.trim() || `地点${state.points.length + 1}`;
  const lat = parseFloat($('latInput').value);
  const lng = parseFloat($('lngInput').value);
  if (Number.isNaN(lat) || Number.isNaN(lng)) { alert('纬度/经度要填数字'); return; }
  addPoint(name, lat, lng);
  $('nameInput').value = $('latInput').value = $('lngInput').value = '';
};

$('planBtn').onclick = plan;

// 启动
renderList();
loadMap();
