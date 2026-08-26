// 页面所有可变状态集中在一个对象里,方便理清"数据从哪来、去哪了"。
const state = {
  points: [],      // 用户加的地点 [{name, lat, lng}],第 0 个当起点
  legs: [],        // 每段出行方式:legs[i] = 第 i 站 → 第 i+1 站的方式
  markers: [],     // 地图上的标记对象,和 points 一一对应
  polylines: [],   // 路线折线(每段一条,各段不同颜色)
  map: null,
  // key 存在浏览器 localStorage:这是使用者自己的 key,不进代码、不进仓库(开源要求)
  mapKey: localStorage.getItem('amap_key') || '',
  // 安全密钥:控制台开启了「安全密钥」就要配,否则地图不渲染(JS API 2.0)
  mapJscode: localStorage.getItem('amap_jscode') || '',
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
  // 开了安全密钥:必须在加载脚本前告诉高德 jscode,否则地图拒绝渲染
  if (state.mapJscode) window._AMapSecurityConfig = { securityJsCode: state.mapJscode };
  const script = document.createElement('script');
  script.src = `https://webapi.amap.com/maps?v=2.0&key=${state.mapKey}`;
  script.onload = initMap;
  script.onerror = () => {
    $('mapHint').textContent = 'key 无效或未加入域名白名单,改用手动输入。';
  };
  document.head.appendChild(script);
}

// ---------- 搜索添加:地名 → 坐标(地理编码) ----------
// 用户输入"广州塔",后端 /search 帮我们翻译成坐标,点一下即添加。
// 注意:搜索不走高德 JS API,而是走自家后端——key 藏后端(前端不碰第三方凭据),
// 还能统一加缓存/日志。这就是"API 代理"模式:前端 → 自己的后端 → 高德。
async function searchPlaces() {
  const kw = $('searchInput').value.trim();
  if (!kw) return;
  // 城市参数可选:填了限定该城市(结果更准),留空 = 全国搜索
  const city = $('cityInput').value.trim();
  const url = '/search?q=' + encodeURIComponent(kw) + (city ? '&city=' + encodeURIComponent(city) : '');
  const resp = await fetch(url);
  const data = await resp.json();
  if (!resp.ok) { alert(data.error || '搜索失败'); return; }

  // 渲染候选列表:名称 + 地址,点一下 addPoint(坐标由后端给出)
  $('searchResults').innerHTML = '';
  for (const poi of data.places) {
    const li = document.createElement('li');
    li.innerHTML = `<div>${poi.name}</div><div class="addr">${poi.address || ''}</div>`;
    li.onclick = () => {
      addPoint(poi.name, poi.lat, poi.lng);
      $('searchInput').value = '';
      $('searchResults').innerHTML = '';
      $('searchInput').focus();
    };
    $('searchResults').appendChild(li);
  }
}

$('searchBtn').onclick = searchPlaces;
$('searchInput').addEventListener('keydown', (e) => { if (e.key === 'Enter') searchPlaces(); });

// ---------- 点的新增 / 删除 ----------
function addPoint(name, lat, lng) {
  state.points.push({ name, lat, lng });
  state.legs.push('driving'); // 新点默认下一段驾车
  if (state.map) {
    const marker = new AMap.Marker({ position: [lng, lat], title: name });
    state.map.add(marker);
    state.markers.push(marker);
  }
  renderList();
}

function removePoint(i) {
  state.points.splice(i, 1);
  state.legs.splice(i, 1); // 删点同时删对应的段方式
  if (state.map && state.markers[i]) state.map.remove(state.markers[i]);
  state.markers.splice(i, 1);
  renderList();
}

// 把地图上已有的点重新描一遍(比如:没有 key 时手动加的点,填 key 后要补上标记)。
// ⚠️ 注意:这里直接 new Marker,绝不能调 addPoint——addPoint 会 push 进 state.points,
// 在列表非空时调用会把所有点再复制一遍(曾经的 3→6 bug)。
function redrawMarkers() {
  if (!state.map) return;
  state.markers.forEach((m) => state.map.remove(m));
  state.markers = [];
  state.points.forEach((p) => {
    const marker = new AMap.Marker({ position: [p.lng, p.lat], title: p.name });
    state.map.add(marker);
    state.markers.push(marker);
  });
}

// 把地点列表画到左侧面板。结构是"地点 / 段连接线 / 地点 / 段连接线 / ...",
// 段选择按钮放在两个地点之间,视觉上就是两点间的连接线。
// 拖拽排序交给 SortableJS(只拖地点 li,段连接线不参与拖拽,拖完 renderList 重排)。
function renderList() {
  const manual = $('manualCheck').checked;
  $('pointList').innerHTML = '';
  state.points.forEach((p, i) => {
    // 健壮性:跳过脏数据(之前拖拽索引 bug 可能混入 undefined,刷新后消失)
    if (!p) return;
    // —— 地点行 ——
    const li = document.createElement('li');
    li.innerHTML = `<span class="badge">${i === 0 ? '起点' : '第' + i + '站'}</span>
      <span>${p.name}</span>
      <button onclick="removePoint(${i})">✕</button>`;
    $('pointList').appendChild(li);

    // —— 段连接线:第 i 站 → 第 i+1 站的方式(最后一个点没有"下一段") ——
    if (i < state.points.length - 1) {
      const leg = document.createElement('div');
      leg.className = 'leg-connector';
      if (!manual) leg.classList.add('hidden'); // 自动模式(TSP)下不显示
      leg.innerHTML = ['driving', 'walking', 'transit'].map((m) =>
        `<button class="leg-btn${state.legs[i] === m ? ' active' : ''}" data-mode="${m}">${modeIcon(m)}</button>`
      ).join('');
      leg.querySelectorAll('.leg-btn').forEach((btn) => {
        btn.onclick = () => {
          state.legs[i] = btn.dataset.mode;
          leg.querySelectorAll('.leg-btn').forEach((b) => b.classList.toggle('active', b === btn));
        };
      });
      $('pointList').appendChild(leg);
    }
  });
}

// 出行方式图标(段连接线小按钮用)
function modeIcon(m) {
  return { driving: '🚗', walking: '🚶', transit: '🚌' }[m] || '🚗';
}

// 出行方式颜色(图例 + 路线画线用同一套,颜色即语义)
function modeColor(m) {
  return { driving: '#1a73e8', walking: '#34a853', transit: '#f9a825' }[m] || '#1a73e8';
}

// SortableJS 接管拖拽:体验远好于手写 HTML5 DnD(动画/触摸/边界情况都处理好)。
// onEnd 里 Sortable 已经把 DOM 移好了,我们同步数据数组。
// ⚠️ renderList() 会重建所有 li(旧元素脱离 DOM),Sortable 持有的元素引用会失效,
// 所以重建后必须 destroy 再 new 重新绑定——这就是 initSortable 可重入的原因。
let sortable = null;
function initSortable() {
  if (sortable) sortable.destroy();
  sortable = new Sortable($('pointList'), {
    animation: 150,        // 拖拽动画,手感
    draggable: 'li',       // 只拖地点行;段连接线(div)不参与拖拽
    handle: 'li',
    onEnd: (evt) => {
      // ⚠️ 必须用 oldDraggableIndex/newDraggableIndex,而不是 oldIndex/newIndex:
      // 列表里混着 li(地点)和 div(段连接线),oldIndex 是"所有子元素"的位置,
      // 会把段连接线也算进去导致越界(splice 出 undefined 混进 points)。
      // DraggableIndex 只算可拖拽的 li,正好对应 state.points。
      const [moved] = state.points.splice(evt.oldDraggableIndex, 1);
      state.points.splice(evt.newDraggableIndex, 0, moved);
      // 段方式跟着点走太复杂(每段连接的是相邻两点),排序后重置为驾车,
      // 用户重排后再手动选。教学简化,注释说明。
      state.legs = state.points.slice(0, -1).map(() => 'driving');
      renderList();       // 列表顺序变了(段连接线也按新顺序重排)
      redrawMarkers();    // 标记重画(和列表对得上)
      initSortable();     // 列表重建后重新绑定拖拽
    },
  });
}

// 勾选/取消"手动设置"→ 显示/隐藏段连接线(TSP 模式下段由算法决定,不显示)
$('manualCheck').onchange = () => renderList();

// ---------- 规划:调后端 /plan(同源,相对路径,前端不需要知道端口) ----------
async function plan() {
  if (state.points.length < 2) { alert('至少需要一个起点 + 一个目的地'); return; }
  const manual = $('manualCheck').checked;
  const resp = await fetch('/plan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      origin: state.points[0],
      destinations: state.points.slice(1),
      manual,
      // 手动模式(默认):每段各自的方式(混合出行),后端逐段算
      segments: manual ? state.legs.slice(0, state.points.length - 1) : undefined,
    }),
  });
  const data = await resp.json();
  if (!resp.ok) { alert(data.error || '规划失败'); return; }

  $('result').innerHTML = '顺序:' +
    data.order.map((n, i) => `<b>${i + 1}. ${n}</b>`).join(' → ') +
    `<br>总距离:<b>${data.total_km.toFixed(1)} km</b>`;
  drawRoute(data.order);
}

// 按后端返回的顺序,把坐标连成折线画在地图上。
// 路线轨迹走后端 /route 代理(和搜索一个套路:key 藏后端,统一支持各出行方式)。
// 每段画一条独立折线,颜色 = 该段出行方式的颜色(和图例一致,颜色即语义)。
async function drawRoute(order) {
  if (!state.map) return;
  const manual = $('manualCheck').checked;
  // 清掉旧路线
  state.polylines.forEach((p) => state.map.remove(p));
  state.polylines = [];

  for (let i = 0; i < order.length - 1; i++) {
    const a = pointByName(order[i]);
    const b = pointByName(order[i + 1]);
    // 手动模式:每段用各自的方式画(混合出行);自动模式(TSP):统一驾车
    const legMode = manual ? (state.legs[i] || 'driving') : 'driving';
    let path = null;
    try {
      const resp = await fetch(`/route?origin=${a.lng},${a.lat}&dest=${b.lng},${b.lat}&mode=${legMode}`);
      if (resp.ok) {
        const data = await resp.json();
        path = data.polyline;
      }
    } catch (err) {
      console.warn('route fetch failed:', err);
    }
    // 拿不到轨迹(公交/网络失败):降级画直线,别让整条线断掉。
    // 和后端"高德失败降级 haversine"是同一个思想。
    if (!path || path.length < 2) path = [[a.lng, a.lat], [b.lng, b.lat]];

    // 每段一条线:颜色按方式,半透明避免盖住地图底图(用户反馈"太不透明")
    const pl = new AMap.Polyline({
      path,
      strokeColor: modeColor(legMode),
      strokeWeight: 5,
      strokeOpacity: 0.7,
      lineJoin: 'round',
    });
    state.map.add(pl);
    state.polylines.push(pl);
  }
  $('legend').classList.remove('hidden'); // 画完路线才显示图例
  state.map.setFitView(); // 缩放到能装下所有标记
}

function pointByName(name) {
  return state.points.find((p) => p.name === name);
}

// ---------- 设置:换 key ----------
$('settingsBtn').onclick = () => {
  $('keyInput').value = state.mapKey;
  $('jscodeInput').value = state.mapJscode;
  $('settingsOverlay').classList.remove('hidden');
};
$('closeSettingsBtn').onclick = () => $('settingsOverlay').classList.add('hidden');
$('saveKeyBtn').onclick = () => {
  const key = $('keyInput').value.trim();
  if (!key) { alert('key 不能为空'); return; }
  localStorage.setItem('amap_key', key);
  localStorage.setItem('amap_jscode', $('jscodeInput').value.trim());
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
initSortable();
loadMap();
