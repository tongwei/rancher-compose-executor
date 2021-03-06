package parser

import (
	"fmt"
	"path"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/rancher-compose-executor/config"
	"github.com/rancher/rancher-compose-executor/lookup"
	"github.com/rancher/rancher-compose-executor/utils"
)

// mergeServicesV1 merges a v1 compose file into an existing set of service configs
func mergeServicesV1(vars map[string]string, resourceLookup lookup.ResourceLookup, file string, datas config.RawServiceMap) (map[string]*config.ServiceConfigV1, error) {
	if err := validate(datas); err != nil {
		return nil, err
	}

	for name, data := range datas {
		var err error
		datas[name], err = parseV1(resourceLookup, vars, file, data, datas)
		if err != nil {
			logrus.Errorf("Failed to parse service %s: %v", name, err)
			return nil, err
		}
	}

	serviceConfigs := make(map[string]*config.ServiceConfigV1)
	if err := utils.Convert(datas, &serviceConfigs); err != nil {
		return nil, err
	}

	return serviceConfigs, nil
}

func parseV1(resourceLookup lookup.ResourceLookup, vars map[string]string, inFile string, serviceData config.RawService, datas config.RawServiceMap) (config.RawService, error) {
	serviceData, err := readEnvFile(resourceLookup, inFile, serviceData)
	if err != nil {
		return nil, err
	}

	serviceData = resolveContextV1(inFile, serviceData)

	value, ok := serviceData["extends"]
	if !ok {
		return serviceData, nil
	}

	mapValue, ok := value.(map[interface{}]interface{})
	if !ok {
		return serviceData, nil
	}

	if resourceLookup == nil {
		return nil, fmt.Errorf("Can not use extends in file %s no mechanism provided to files", inFile)
	}

	file := asString(mapValue["file"])
	service := asString(mapValue["service"])

	if service == "" {
		return serviceData, nil
	}

	var baseService config.RawService

	if file == "" {
		if serviceData, ok := datas[service]; ok {
			baseService, err = parseV1(resourceLookup, vars, inFile, serviceData, datas)
		} else {
			return nil, fmt.Errorf("Failed to find service %s to extend", service)
		}
	} else {
		bytes, resolved, err := resourceLookup.Lookup(file, inFile)
		if err != nil {
			logrus.Errorf("Failed to lookup file %s: %v", file, err)
			return nil, err
		}

		rawConfig, err := createRawConfig(bytes)
		if err != nil {
			return nil, err
		}
		baseRawServices := rawConfig.Services

		if err = interpolateRawServiceMap(&baseRawServices, vars); err != nil {
			return nil, err
		}

		baseRawServices, err = preProcessServiceMap(baseRawServices)
		if err != nil {
			return nil, err
		}

		if err := validate(baseRawServices); err != nil {
			return nil, err
		}

		baseService, ok = baseRawServices[service]
		if !ok {
			return nil, fmt.Errorf("Failed to find service %s in file %s", service, file)
		}

		baseService, err = parseV1(resourceLookup, vars, resolved, baseService, baseRawServices)
		if err != nil {
			return nil, err
		}
	}

	baseService = clone(baseService)

	logrus.Debugf("Merging %#v, %#v", baseService, serviceData)

	for _, k := range noMerge {
		if _, ok := baseService[k]; ok {
			source := file
			if source == "" {
				source = inFile
			}
			return nil, fmt.Errorf("Cannot extend service '%s' in %s: services with '%s' cannot be extended", service, source, k)
		}
	}

	baseService = mergeConfig(baseService, serviceData)

	logrus.Debugf("Merged result %#v", baseService)

	return baseService, nil
}

func resolveContextV1(inFile string, serviceData config.RawService) config.RawService {
	context := asString(serviceData["build"])
	if context == "" {
		return serviceData
	}

	if IsValidRemote(context) {
		return serviceData
	}

	current := path.Dir(inFile)

	if context == "." {
		context = current
	} else {
		current = path.Join(current, context)
	}

	serviceData["build"] = current

	return serviceData
}
