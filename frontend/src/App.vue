<script lang="ts" setup>
import { ref, onMounted, reactive } from 'vue';
import { GetRules, AddRule, UpdateRule, DeleteRule, StartServer, StopServer, TestUDP, GetTrafficStats, ResetTrafficStats } from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';

// 使用从models中导入的类型
type SocksRule = main.SocksRule;

const rules = ref<SocksRule[]>([]);
const showAddModal = ref(false);
const isEditing = ref(false);
const currentEditId = ref('');
const loading = ref(false);
const theme = ref(localStorage.getItem('theme') || 'light');
const udpTestResult = ref('');

const newRule = reactive({
  name: '',
  port: 1080,
  username: '',
  password: '',
  noAuth: false,
  enableUDP: false
});

onMounted(async () => {
  // 加载SOCKS5规则
  await loadRules();
  
  // 初始化主题
  applyTheme();
  
  // 监听系统主题变化
  const darkModeMediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
  const handleChange = () => {
    if (!localStorage.getItem('theme')) {
      theme.value = darkModeMediaQuery.matches ? 'dark' : 'light';
      applyTheme();
    }
  };
  
  darkModeMediaQuery.addEventListener('change', handleChange);
  
  // 定时刷新流量统计（每5秒）
  setInterval(async () => {
    if (rules.value.some(rule => rule.running)) {
      await loadRules();
    }
  }, 5000);

  // 定时刷新流量统计（每10秒）
  setInterval(async () => {
    if (rules.value.some(rule => rule.running)) {
      try {
        // 获取最新的规则列表
        const updatedRules = await GetRules();
        // 更新规则列表
        rules.value = updatedRules;
      } catch (error) {
        console.error('Failed to update traffic stats:', error);
      }
    }
  }, 10000);
});

// 格式化流量大小的函数
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  
  return parseFloat((bytes / Math.pow(1024, i)).toFixed(2)) + ' ' + sizes[i];
}

async function loadRules() {
  loading.value = true;
  try {
    rules.value = await GetRules();
  } catch (error) {
    console.error('Failed to load rules:', error);
  } finally {
    loading.value = false;
  }
}

function resetForm() {
  newRule.name = '';
  newRule.port = 1080;
  newRule.username = '';
  newRule.password = '';
  newRule.noAuth = false;
  newRule.enableUDP = false;
  isEditing.value = false;
  currentEditId.value = '';
}

function openAddModal() {
  resetForm();
  showAddModal.value = true;
}

function closeModal() {
  showAddModal.value = false;
  resetForm();
}

function fillFormForEdit(rule: SocksRule) {
  newRule.name = rule.name;
  newRule.port = rule.port;
  newRule.username = rule.username;
  newRule.password = rule.password;
  newRule.noAuth = rule.noAuth;
  newRule.enableUDP = rule.enableUDP;
  isEditing.value = true;
  currentEditId.value = rule.id;
  showAddModal.value = true;
}

async function saveRule() {
  if (!newRule.name || newRule.port <= 0 || newRule.port > 65535) {
    alert('请填写有效的名称和端口');
    return;
  }

  if (!newRule.noAuth && (!newRule.username || !newRule.password)) {
    alert('请填写用户名和密码，或选择无授权模式');
    return;
  }

  loading.value = true;
  try {
    if (isEditing.value) {
      // 查找原有规则的流量数据
      const existingRule = rules.value.find(r => r.id === currentEditId.value);
      
      // 更新规则，保留现有流量数据和运行状态
      const updatedRule: SocksRule = {
        id: currentEditId.value,
        name: newRule.name,
        port: newRule.port,
        username: newRule.username,
        password: newRule.password,
        noAuth: newRule.noAuth,
        enableUDP: newRule.enableUDP,
        running: existingRule?.running || false,
        uploadBytes: existingRule?.uploadBytes || 0,
        downloadBytes: existingRule?.downloadBytes || 0
      };
      
      await UpdateRule(updatedRule);
    } else {
      // 添加新规则
      const rule: SocksRule = {
        id: '',
        name: newRule.name,
        port: newRule.port,
        username: newRule.username,
        password: newRule.password,
        noAuth: newRule.noAuth,
        enableUDP: newRule.enableUDP,
        running: false,
        uploadBytes: 0,
        downloadBytes: 0
      };
      
      await AddRule(rule);
    }
    
    closeModal();
    await loadRules();
  } catch (error) {
    console.error('Failed to save rule:', error);
    alert('保存规则失败');
  } finally {
    loading.value = false;
  }
}

async function deleteRule(id: string) {
  if (!confirm('确定要删除这条规则吗？')) {
    return;
  }
  
  loading.value = true;
  try {
    await DeleteRule(id);
    await loadRules();
  } catch (error) {
    console.error('Failed to delete rule:', error);
    alert('删除规则失败');
  } finally {
    loading.value = false;
  }
}

async function toggleServer(rule: SocksRule) {
  loading.value = true;
  try {
    if (rule.running) {
      await StopServer(rule.id);
    } else {
      await StartServer(rule.id);
    }
    await loadRules();
  } catch (error) {
    console.error('Failed to toggle server status:', error);
    alert(rule.running ? '停止服务器失败' : '启动服务器失败');
  } finally {
    loading.value = false;
  }
}

// 测试UDP功能
async function testUDP(rule: SocksRule) {
  if (!rule.running || !rule.enableUDP) {
    alert('请确保服务器已启动并启用了UDP转发');
    return;
  }
  
  try {
    udpTestResult.value = await TestUDP(rule.id);
    alert(udpTestResult.value);
  } catch (error) {
    console.error('UDP测试失败:', error);
    alert('UDP测试失败: ' + error);
  }
}

function toggleTheme() {
  theme.value = theme.value === 'light' ? 'dark' : 'light';
  localStorage.setItem('theme', theme.value);
  applyTheme();
}

function applyTheme() {
  if (theme.value === 'dark') {
    document.documentElement.classList.add('dark');
  } else {
    document.documentElement.classList.remove('dark');
  }
}

// 重置指定规则的流量统计
async function resetRuleTraffic(id: string) {
  try {
    await ResetTrafficStats(id);
    await loadRules(); // 重新加载规则以更新流量显示
  } catch (error) {
    console.error('Failed to reset traffic stats:', error);
    alert('重置流量统计失败');
  }
}
</script>

<template>
  <div class="min-h-screen bg-gray-100 dark:bg-gray-900 text-gray-800 dark:text-white">
    <!-- 顶部导航栏 -->
    <div class="bg-white dark:bg-gray-800 shadow-md p-4">
      <div class="container mx-auto flex justify-between items-center">
        <div class="flex items-center">
          <h1 class="text-xl font-bold">Gosker SOCKS5 管理器</h1>
          <span class="ml-2 px-2 py-1 text-xs bg-blue-100 dark:bg-blue-800 text-blue-800 dark:text-blue-200 rounded-full">v0.1.4</span>
        </div>
        <button 
          class="p-2 rounded-full bg-gray-200 dark:bg-gray-700"
          @click="toggleTheme"
        >
          {{ theme === 'light' ? '🌙' : '☀️' }}
        </button>
      </div>
    </div>

    <!-- 主内容区 -->
    <div class="container mx-auto p-4">
      <div class="flex justify-between items-center mb-6">
        <h2 class="text-2xl font-bold">SOCKS5 服务器列表</h2>
        <button 
          class="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600 transition"
          @click="openAddModal"
        >
          添加规则
        </button>
      </div>

      <!-- 功能说明 -->
      <div class="bg-blue-50 dark:bg-blue-900 p-4 rounded-lg mb-6 text-sm">
        <h3 class="font-semibold mb-2">关于SOCKS5代理</h3>
        <p class="mb-2">SOCKS5是一种网络代理协议，支持TCP和UDP通信。本应用使用增强版SOCKS5库，提供更好的UDP转发支持。</p>
        <ul class="list-disc list-inside mb-2">
          <li><span class="font-semibold">TCP模式</span>：支持HTTP、HTTPS等基于TCP的协议</li>
          <li><span class="font-semibold">UDP模式</span>：支持DNS查询、游戏、VoIP等UDP协议，通过增强库提供稳定的UDP转发</li>
        </ul>
        <p class="text-xs text-blue-700 dark:text-blue-300">
          使用提示：
          <br>• 开启UDP支持前，请确保你的防火墙允许相应端口的UDP流量
          <br>• 使用支持SOCKS5 UDP的客户端（如SocksCap64、Proxifier等）
          <br>• 某些客户端可能需要特殊配置以启用UDP功能，请参考相应的客户端文档
          <br>• <b>注意</b>：由于UDP协议的特性，在netstat命令中可能不会显示UDP连接状态，这是正常现象。UDP是无连接协议，只有在活跃传输数据时才会短暂建立状态。
        </p>
      </div>

      <!-- 加载提示 -->
      <div v-if="loading" class="flex justify-center my-8">
        <div class="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500"></div>
      </div>

      <!-- 规则列表 -->
      <div v-else-if="rules.length === 0" class="bg-white dark:bg-gray-800 rounded-lg shadow-xl p-8 text-center">
        <p class="text-xl mb-4">暂无SOCKS5规则</p>
        <button 
          class="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600 transition"
          @click="openAddModal"
        >
          创建第一条规则
        </button>
      </div>

      <div v-else class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <div 
          v-for="rule in rules" 
          :key="rule.id" 
          class="bg-white dark:bg-gray-800 rounded-lg shadow-lg overflow-hidden"
        >
          <div class="p-4">
            <div class="flex justify-between items-start">
              <h3 class="text-lg font-bold">{{ rule.name }}</h3>
              <span 
                class="px-2 py-1 rounded-full text-xs font-semibold"
                :class="rule.running ? 'bg-green-100 text-green-800 dark:bg-green-800 dark:text-green-100' : 'bg-red-100 text-red-800 dark:bg-red-800 dark:text-red-100'"
              >
                {{ rule.running ? '运行中' : '已停止' }}
              </span>
            </div>
            
            <div class="py-2">
              <p class="text-sm"><span class="font-semibold">端口:</span> {{ rule.port }}</p>
              <p class="text-sm"><span class="font-semibold">认证方式:</span> {{ rule.noAuth ? '无授权' : '用户/密码' }}</p>
              <p v-if="!rule.noAuth" class="text-sm">
                <span class="font-semibold">用户名:</span> {{ rule.username }}
              </p>
              <p class="text-sm">
                <span class="font-semibold">UDP转发:</span> 
                <span :class="rule.enableUDP ? 'text-green-600 dark:text-green-400' : 'text-gray-600 dark:text-gray-400'">
                  {{ rule.enableUDP ? '已启用' : '已禁用' }}
                </span>
              </p>
            </div>
            
            <!-- 流量统计部分 -->
            <div class="mt-3 p-2 bg-gray-50 dark:bg-gray-700 rounded-md">
              <div class="flex justify-between items-center mb-2">
                <h4 class="text-sm font-semibold">流量统计</h4>
                <button 
                  v-if="rule.uploadBytes > 0 || rule.downloadBytes > 0"
                  class="text-xs text-gray-500 dark:text-gray-400 hover:text-red-500 dark:hover:text-red-400"
                  @click="resetRuleTraffic(rule.id)"
                  title="重置流量统计"
                >
                  <span>重置</span>
                </button>
              </div>
              <div class="grid grid-cols-2 gap-2 text-xs">
                <div class="flex flex-col">
                  <span class="text-blue-500 dark:text-blue-400">上传</span>
                  <span class="font-mono">{{ formatBytes(rule.uploadBytes) }}</span>
                </div>
                <div class="flex flex-col">
                  <span class="text-green-500 dark:text-green-400">下载</span>
                  <span class="font-mono">{{ formatBytes(rule.downloadBytes) }}</span>
                </div>
              </div>
            </div>
            
            <div class="flex justify-end mt-4 space-x-2">
              <button 
                class="px-3 py-1 border border-gray-300 dark:border-gray-600 rounded text-sm hover:bg-gray-100 dark:hover:bg-gray-700 transition"
                @click="fillFormForEdit(rule)"
              >
                编辑
              </button>
              <button 
                class="px-3 py-1 rounded text-sm text-white transition"
                :class="rule.running ? 'bg-red-500 hover:bg-red-600' : 'bg-green-500 hover:bg-green-600'"
                @click="toggleServer(rule)"
              >
                {{ rule.running ? '停止' : '启动' }}
              </button>
              <button 
                v-if="rule.enableUDP && rule.running"
                class="px-3 py-1 border border-blue-500 text-blue-500 rounded text-sm hover:bg-blue-50 dark:hover:bg-blue-900 transition"
                @click="testUDP(rule)"
              >
                测试UDP
              </button>
              <button 
                class="px-3 py-1 border border-red-500 text-red-500 rounded text-sm hover:bg-red-50 dark:hover:bg-red-900 transition"
                @click="deleteRule(rule.id)"
              >
                删除
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 添加/编辑规则模态框 -->
    <div v-if="showAddModal" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center p-4 z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md">
        <div class="p-6">
          <h3 class="text-lg font-bold mb-4">{{ isEditing ? '编辑规则' : '添加新规则' }}</h3>
          
          <div class="mb-4">
            <label class="block text-sm font-medium mb-1">规则名称</label>
            <input 
              type="text" 
              v-model="newRule.name" 
              placeholder="例如: 我的SOCKS5代理" 
              class="w-full px-3 py-2 border rounded dark:bg-gray-700 dark:border-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>
          
          <div class="mb-4">
            <label class="block text-sm font-medium mb-1">端口</label>
            <input 
              type="number" 
              v-model="newRule.port" 
              placeholder="1080" 
              class="w-full px-3 py-2 border rounded dark:bg-gray-700 dark:border-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
              min="1"
              max="65535"
            />
          </div>
          
          <div class="mb-4 flex items-center">
            <label class="inline-flex items-center cursor-pointer">
              <input 
                type="checkbox" 
                v-model="newRule.noAuth" 
                class="form-checkbox h-5 w-5 text-blue-500"
              />
              <span class="ml-2">无授权模式</span>
            </label>
          </div>

          <div class="mb-4 flex items-center">
            <label class="inline-flex items-center cursor-pointer">
              <input 
                type="checkbox" 
                v-model="newRule.enableUDP" 
                class="form-checkbox h-5 w-5 text-blue-500"
              />
              <span class="ml-2">启用UDP转发</span>
            </label>
            <span class="ml-2 text-xs text-gray-500 dark:text-gray-400">
              (支持DNS查询、游戏、VoIP等UDP协议)
            </span>
          </div>

          <div v-if="!newRule.noAuth" class="space-y-4">
            <div>
              <label class="block text-sm font-medium mb-1">用户名</label>
              <input 
                type="text" 
                v-model="newRule.username" 
                placeholder="用户名" 
                class="w-full px-3 py-2 border rounded dark:bg-gray-700 dark:border-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            
            <div>
              <label class="block text-sm font-medium mb-1">密码</label>
              <input 
                type="password" 
                v-model="newRule.password" 
                placeholder="密码" 
                class="w-full px-3 py-2 border rounded dark:bg-gray-700 dark:border-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>
          
          <div class="flex justify-end mt-6 space-x-3">
            <button 
              class="px-4 py-2 border border-gray-300 rounded hover:bg-gray-100 dark:border-gray-600 dark:hover:bg-gray-700 transition"
              @click="closeModal"
            >
              取消
            </button>
            <button 
              class="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600 transition"
              @click="saveRule"
            >
              保存
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
