package ffiwrapper

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type TaskType int

const (
	APTask TaskType = iota
	P1Task
	P2Task
	C2Task
)

type Configurations struct {
	AP []TaskConfig `yaml:"AP"`
	P1 []TaskConfig `yaml:"P1"`
	P2 []TaskConfig `yaml:"P2"`
	C2 []TaskConfig `yaml:"C2"`
}

type TaskConfig struct {
	CPUSet     string   `yaml:"cpuset"`
	GPU        string   `yaml:"gpu"`
	Env        []string `yaml:"env"`
	Concurrent int      `yaml:"concurrent"`
	Supra      bool     `yaml:"supra"`
	Bin        string   `yaml:"bin"`
}

type ResourceUnit struct {
	Type       TaskType
	Index      int
	CPUSet     string
	GPU        string
	Env        []string
	Concurrent int
	Supra      bool
	Bin        string
}

type ResourceManager struct {
	configs        Configurations
	resourcePool   map[TaskType][]ResourceUnit
	allocatedUnits map[TaskType]map[int]bool // 跟踪已分配的资源单元
	resourceLock   sync.Mutex
	initialized    uint32
	once           sync.Once
}
var (
	globalResourceManager = &ResourceManager{}
)

func GetGlobalResourceManager() *ResourceManager {
	if atomic.LoadUint32(&globalResourceManager.initialized) == 0 {
		globalResourceManager.once.Do(func() {
			globalResourceManager.resourcePool = make(map[TaskType][]ResourceUnit)
			globalResourceManager.allocatedUnits = make(map[TaskType]map[int]bool)
			for _, taskType := range []TaskType{APTask, P1Task, P2Task, C2Task} {
				globalResourceManager.allocatedUnits[taskType] = make(map[int]bool)
			}
			if config := os.Getenv("TASK_CONFIG"); config != "" {
				log.Infof("use new task config :%+v", config)
				err := globalResourceManager.LoadConfigFile(config)
				if err != nil {
					log.Fatal(err)
					return
				}
				globalResourceManager.initResourcePool()
			}
			atomic.StoreUint32(&globalResourceManager.initialized, 1)
		})
	}
	return globalResourceManager
}

// 正则表达式验证配置项合法性
func validateConfig(config TaskConfig) bool {
	cpuSetPattern := regexp.MustCompile(`^\d+(?:-\d+)?(?:,\d+(?:-\d+)?)*$`)
	gpuPattern := regexp.MustCompile(`^\d+(,\d+)*$`)
	binPattern := regexp.MustCompile(`^/.+$`)

	if !cpuSetPattern.MatchString(config.CPUSet) {
		return false
	}

	if config.GPU != "" && !gpuPattern.MatchString(config.GPU) {
		return false
	}

	if config.Bin != "" && !binPattern.MatchString(config.Bin) {
		return false
	}

	return true
}

func (rm *ResourceManager) LoadConfigFile(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &rm.configs)
	if err != nil {
		return err
	}

	for _, config := range rm.configs.AP {
		if !validateConfig(config) {
			return fmt.Errorf("invalid config for AP task")
		}
	}
	// ... (验证其他任务类型的配置)

	return nil
}

// 解析 cpuset 配置
func parseCPUSet(cpuset string) []string {
	cpuSetItems := []string{}
	ranges := strings.Split(cpuset, ",")
	for _, r := range ranges {
		if strings.Contains(r, "-") {
			rangeParts := strings.Split(r, "-")
			start, _ := strconv.Atoi(rangeParts[0])
			end, _ := strconv.Atoi(rangeParts[1])
			for i := start; i <= end; i++ {
				cpuSetItems = append(cpuSetItems, strconv.Itoa(i))
			}
		} else {
			cpuSetItems = append(cpuSetItems, r)
		}
	}
	return cpuSetItems
}

// 初始化资源池
func (rm *ResourceManager) initResourcePool() {
	rm.resourceLock.Lock()
	defer rm.resourceLock.Unlock()

	for _, config := range rm.configs.AP {
		units := rm.generateResourceUnits(APTask, config)
		rm.resourcePool[APTask] = append(rm.resourcePool[APTask], units...)
	}
	rm.rebuildIndexForTaskType(APTask)

	for _, config := range rm.configs.P1 {
		units := rm.generateResourceUnits(P1Task, config)
		rm.resourcePool[P1Task] = append(rm.resourcePool[P1Task], units...)
	}
	rm.rebuildIndexForTaskType(P1Task)

	for _, config := range rm.configs.P2 {
		units := rm.generateResourceUnits(P2Task, config)
		rm.resourcePool[P2Task] = append(rm.resourcePool[P2Task], units...)
	}
	rm.rebuildIndexForTaskType(P2Task)

	for _, config := range rm.configs.C2 {
		units := rm.generateResourceUnits(C2Task, config)
		rm.resourcePool[C2Task] = append(rm.resourcePool[C2Task], units...)
	}
	rm.rebuildIndexForTaskType(C2Task)

}
func (rm *ResourceManager) rebuildIndexForTaskType(taskType TaskType) {
	switch taskType {
	case APTask, P1Task, P2Task, C2Task:
		units, found := rm.resourcePool[taskType]
		if !found || len(units) == 0 {
			return
		}
		for index := range units {
			rm.resourcePool[taskType][index].Index = index
		}
		return
	default:
		log.Error("invalid task type")
	}
}

// 生成资源单元
func (rm *ResourceManager) generateResourceUnits(taskType TaskType, config TaskConfig) []ResourceUnit {

	var resourceUnits []ResourceUnit
	cpuSetItems := parseCPUSet(config.CPUSet)

	if len(cpuSetItems) == 0 {
		return resourceUnits
	}

	numCPUs := len(cpuSetItems)
	concurrent := config.Concurrent

	if concurrent <= 0 {
		return resourceUnits
	}

	if numCPUs <= concurrent {
		for i := 0; i < concurrent; i++ {
			resourceUnits = append(resourceUnits, ResourceUnit{
				Type:       taskType,
				Index:      i,
				CPUSet:     strings.Join(cpuSetItems, ","),
				GPU:        config.GPU,
				Env:        config.Env,
				Concurrent: 1,
				Supra:      config.Supra,
				Bin:        config.Bin,
			})
		}
		return resourceUnits
	}

	expandedCPUs := expandCPUs(cpuSetItems, concurrent)
	cpusPerUnit := len(expandedCPUs) / concurrent

	start := 0
	for i := 0; i < concurrent; i++ {
		end := start + cpusPerUnit
		unitCPUs := expandedCPUs[start:end]
		unit := ResourceUnit{
			Type:       taskType,
			Index:      i,
			CPUSet:     strings.Join(removeDuplicates(unitCPUs), ","),
			GPU:        config.GPU,
			Env:        config.Env,
			Concurrent: 1,
			Supra:      config.Supra,
			Bin:        config.Bin,
		}
		resourceUnits = append(resourceUnits, unit)
		start = end
	}
	return resourceUnits
}
func (rm *ResourceManager) markResourceUnitAllocated(taskType TaskType, index int) {
	rm.allocatedUnits[taskType][index] = true
}

func (rm *ResourceManager) markResourceUnitUnallocated(taskType TaskType, index int) {
	rm.allocatedUnits[taskType][index] = false
}

func removeDuplicates(input []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, item := range input {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

func expandCPUs(cpus []string, count int) []string {
	expanded := make([]string, 0, len(cpus)*count)
	for _, cpu := range cpus {
		for i := 0; i < count; i++ {
			expanded = append(expanded, cpu)
		}
	}
	return expanded
}

// 列出每个任务类型占用的资源
func (rm *ResourceManager) ListTaskResources() map[TaskType][]ResourceUnit {
	rm.resourceLock.Lock()
	defer rm.resourceLock.Unlock()

	result := make(map[TaskType][]ResourceUnit)
	for taskType, units := range rm.resourcePool {
		result[taskType] = units
	}
	return result
}

// 列出所有已分配的资源
func (rm *ResourceManager) ListAllocatedResources() []ResourceUnit {
	rm.resourceLock.Lock()
	defer rm.resourceLock.Unlock()

	var allocatedResources []ResourceUnit
	for taskType := range rm.resourcePool {
		units, found := rm.resourcePool[taskType]
		if !found || len(units) == 0 {
			continue
		}
		for _, unit := range units {
			if rm.allocatedUnits[unit.Type][unit.Index] {
				allocatedResources = append(allocatedResources, unit)
			}
		}
	}
	return allocatedResources
}
func (rm *ResourceManager) ListUnallocatedResources() []ResourceUnit {
	rm.resourceLock.Lock()
	defer rm.resourceLock.Unlock()

	var unallocatedResources []ResourceUnit
	for taskType := range rm.resourcePool {
		units, found := rm.resourcePool[taskType]
		if !found || len(units) == 0 {
			continue
		}
		for _, unit := range units {
			if !rm.allocatedUnits[unit.Type][unit.Index] {
				unallocatedResources = append(unallocatedResources, unit)
			}
		}
	}
	return unallocatedResources
}
func (rm *ResourceManager) ListUnallocatedResourcesForTaskType(taskType TaskType) ([]ResourceUnit, error) {
	rm.resourceLock.Lock()
	defer rm.resourceLock.Unlock()

	switch taskType {
	case APTask, P1Task, P2Task, C2Task:
		units, found := rm.resourcePool[taskType]
		if !found || len(units) == 0 {
			return nil, nil
		}

		var unallocatedResources []ResourceUnit
		for _, unit := range units {
			if !rm.allocatedUnits[unit.Type][unit.Index] {
				unallocatedResources = append(unallocatedResources, unit)
			}
		}
		return unallocatedResources, nil

	default:
		return nil, fmt.Errorf("invalid task type")
	}
}
func (rm *ResourceManager) ListAllocatedResourcesForTaskType(taskType TaskType) ([]ResourceUnit, error) {
	rm.resourceLock.Lock()
	defer rm.resourceLock.Unlock()
	switch taskType {
	case APTask, P1Task, P2Task, C2Task:
		units, found := rm.resourcePool[taskType]
		if !found || len(units) == 0 {
			return nil, nil
		}
		var allocatedResources []ResourceUnit
		for _, unit := range units {
			if rm.allocatedUnits[unit.Type][unit.Index] {
				allocatedResources = append(allocatedResources, unit)
			}
		}
		return allocatedResources, nil

	default:
		return nil, fmt.Errorf("invalid task type")
	}
}

// 分配资源
func (rm *ResourceManager) AllocateResource(taskType TaskType) (ResourceUnit, error) {
	rm.resourceLock.Lock()
	defer rm.resourceLock.Unlock()
	units, found := rm.resourcePool[taskType]
	if !found || len(units) == 0 {
		return ResourceUnit{}, fmt.Errorf("no available resources for task type %d", taskType)
	}

	for _, unit := range units {
		if !rm.allocatedUnits[unit.Type][unit.Index] {
			rm.markResourceUnitAllocated(unit.Type, unit.Index)
			return unit, nil
		}
	}
	return ResourceUnit{}, fmt.Errorf("no available resources for task type %d", taskType)
}

// 回收资源
func (rm *ResourceManager) ReleaseResource(unit ResourceUnit) {
	rm.resourceLock.Lock()
	defer rm.resourceLock.Unlock()
	// 标记已分配的资源单元为未分配
	rm.markResourceUnitUnallocated(unit.Type, unit.Index)
}

// 配置文件热更新
func (rm *ResourceManager) WatchAndReloadConfigFile(filename string) {
	for {
		select {
		case <-time.After(time.Second * 5): // 每隔5秒检查一次
			data, err := ioutil.ReadFile(filename)
			if err != nil {
				log.Errorf("Error reading config file: %+v", err)
				continue
			}

			var newConfigs Configurations
			err = yaml.Unmarshal(data, &newConfigs)
			if err != nil {
				log.Errorf("Error unmarshaling new config: %+v", err)
				continue
			}
			rm.resourceLock.Lock()
			rm.configs = newConfigs
			rm.initResourcePool()
			rm.resourceLock.Unlock()
		}
	}
}

func InitResource() {
	config, err := json.MarshalIndent(GetGlobalResourceManager().ListTaskResources(), "", "   ")
	if err != nil {
		log.Error(err)
		return
	}
	fmt.Println(string(config))
	log.Info(string(config))
}