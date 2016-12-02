package cli

import (
	"fmt"
	"github.com/forj-oss/forjj-modules/cli/interface"
	"github.com/forj-oss/forjj-modules/trace"
)

type ForjCliContext struct {
	action  *ForjAction          // Can be only one action
	object  *ForjObject          // Can be only one object at a time. Ex: forj add repo
	list    *ForjObjectList      // Can be only one list at a time.
	context clier.ParseContexter // kingpin interface context.
	// forjj add apps ...
}

// LoadContext gets data from context and store it in internal object model (ForjValue)
//
//
func (c *ForjCli) loadContext(args []string, context interface{}) (cmds []clier.CmdClauser, err error) {

	if v, err := c.App.ParseContext(args); v == nil {
		return cmds, err
	} else {
		c.cli_context.context = v
	}

	cmds = c.cli_context.context.SelectedCommands()
	if len(cmds) == 0 {
		err = c.contextHook(context)
		return
	}

	// Determine selected Action/object/object list.
	c.identifyObjects(cmds[len(cmds)-1])

	// Load object list instances
	c.loadListData(nil, c.cli_context.context)

	// Load object data (if envar are set)
	c.loadContextObjectData()

	// Load anything that could be required from any existing flags setup.
	// Ex: app driver - app object hook. - Add new flags/args/objects
	//     Settings of Defaults, flags attributes - Application hook. - Update existing flags.
	// Context hook is started in following order
	// - Application layer.
	// - Object layer.
	// - Object list layer.
	if err = c.contextHook(context); err != nil {
		return
	}

	// Define instance flags for each list.
	c.addInstanceFlags()

	// On existing objects, add defaults values or envars
	//	c.addDefaults()
	return
}

// preload_objects do loading of objects with defaults in c.values[object].records["object"]
/*func (c *ForjCli) addDefaults() {
}*/

// loadContextObjectData create objects from envar/defaults based on cli
func (c *ForjCli) loadContextObjectData() {

}

// ContextHook
// Load anything that could be required from any existing flags setup.
// Ex: app driver - app object hook. - Add new flags/args/objects
//     Settings of Defaults, flags attributes - Application hook. - Update existing flags.
func (c *ForjCli) contextHook(context interface{}) error {
	if c.context_hook != nil {
		if err := c.context_hook(c, context); err != nil {
			return err
		}
	}
	for _, object := range c.objects {
		for _, list := range object.list {
			if list.context_hook == nil {
				continue
			}
			if err := list.context_hook(list, c, context); err != nil {
				object.err = err
				return err
			}
		}

		if object.context_hook == nil {
			continue
		}
		if err := object.context_hook(object, c, context); err != nil {
			object.err = err
			return err
		}
	}

	return nil
}

// check List flag and start creating object instance.
func (c *ForjCli) loadListData(more_flags func(*ForjCli), context clier.ParseContexter) error {
	// check if the ObjectList is found.
	// Ex: forjj create repos <list>
	// <list> is an ArgClauser, repos is a CmdClauser
	if c.cli_context.list != nil {
		gotrace.Trace("Loading Data list from an Object list.")
		l := c.cli_context.list
		if c.cli_context.context != nil {
			// Load context string in the list.
			paramList_name := l.getParamListObjectName()
			if a, ok := l.actions[c.cli_context.action.name].params[paramList_name].(*ForjArgList); ok {
				if v, found := c.cli_context.context.GetArgValue(a.GetArgClauser()); found {
					gotrace.Trace("Initializing context list '%s' with '%s'", l.name, v)
					l.Set(to_string(v))
				}
			}
		}

		key_name := l.obj.getKeyName()
		// loop on list data to create object records.
		for _, attrs := range l.context {
			// Get the list element key
			key_value := attrs.Data[key_name]
			if key_value == "" {
				return fmt.Errorf("Invalid key value for object list '%s-%s'. a key cannot be empty.",
					l.obj.name, l.name)
			}

			data := c.setObjectAttributes(c.cli_context.action.name, l.obj.name, key_value)
			for key, value := range attrs.Data {
				field := l.obj.fields[key]
				if err := data.set(field.value_type, key, value); err != nil {
					return err
				}
				data.attrs[key] = value
			}
		}
		gotrace.Trace("Loading Data list from an Object list flags.")
		return c.updateObjectFromContext(l.actions[c.cli_context.action.name].params)
	}

	// Check if the Object is found
	// Ex: forjj create repo <list> # with any additional object lists flags.
	if c.cli_context.object != nil {
		o := c.cli_context.object
		gotrace.Trace("Loading Data list from the object '%s'.", o.name)
		var key_value string

		key_name := o.getKeyName()
		param := o.actions[c.cli_context.action.name].params[key_name]
		if param == nil {
			return fmt.Errorf("Unable to find key '%s' in object action '%s-%s' parameters.",
				key_name, o.name, c.cli_context.action.name)
		}
		if v, found := c.getContextValue(context, param.(forjParam)); !found {
			return fmt.Errorf("Unable to find key '%s' value from action '%s' parameters. "+
				"Missing OnActions().AddFlag(%s)?", key_name, c.cli_context.action.name, key_name)
		} else {
			key_value = to_string(v)
		}
		if key_value == "" {
			return fmt.Errorf("Invalid key value for object '%s'. a key cannot be empty.", o.name)
		}
		gotrace.Trace("New object record identified by key '%s' (%s).", key_value, o.getKeyName())

		// Search for object list flags
		if err := c.updateObjectFromContext(o.actions[c.cli_context.action.name].params); err != nil {
			return err
		}

		// get or create a record and populate it with all flags/args
		data := c.setObjectAttributes(c.cli_context.action.name, o.name, key_value)
		for field_name, field := range o.fields {
			param := o.actions[c.cli_context.action.name].params[field_name]
			v, _ := c.getContextValue(context, param.(forjParam))
			// even if v is nil, a record is created. But will be considered as not found in Forj*.Get* functions
			if err := data.set(field.value_type, field_name, v); err != nil {
				return err
			}
			param.forjParamUpdater().set_ref(data)
		}
		return nil
	}

	if c.cli_context.action == nil {
		return nil
	}

	// Parse flags to determine if there is another objects list
	gotrace.Trace("Loading Data list from an action flag/arg.")
	return c.updateObjectFromContext(c.cli_context.action.params)
}

// updateObjectFromContext do 2 things:
// - when a List param is detected, load the context and create object data.
// - when an object param is detected, create the single object and add data.
func (c *ForjCli) updateObjectFromContext(params map[string]ForjParam) error {
	for _, param := range params {
		if err := param.forjParamSetter().createObjectDataFromParams(params); err != nil {
			return err
		}
	}
	return nil
}

func (c *ForjCli) getContextValue(context clier.ParseContexter, param forjParam) (interface{}, bool) {
	switch param.(type) {
	case *ForjArg:
		a := param.(*ForjArg)
		return context.GetArgValue(a.arg)
	case *ForjFlag:
		f := param.(*ForjFlag)
		return context.GetFlagValue(f.flag)
	}
	return "", false
}

func (c *ForjCli) addInstanceFlags() {
	for _, l := range c.list {
		if _, found := c.values[l.obj.name]; !found {
			continue
		}
		r := c.values[l.obj.name]
		if len(r.records) == 0 {
			continue
		}
		for instance_name := range r.records {
			for field_name, field := range l.obj.fields {
				found := false
				// Do not include fields defined by the list.
				for _, fname := range l.fields_name {
					if fname == field_name {
						found = true
						break
					}
				}
				if found {
					continue
				}
				// Add instance flags to `<app> <action> <object>s --...`
				flag_name := instance_name + "-" + field_name
				for _, action := range l.actions {
					// Do not recreate if already exist.
					if _, found := action.params[flag_name]; found {
						continue
					}

					f := new(ForjFlag)
					p := ForjParam(f)
					p.set_cmd(action.cmd, field.value_type, flag_name, field.help+" for "+instance_name, nil)
					f.list = l
					f.instance_name = instance_name
					f.field_name = field_name
					action.params[flag_name] = p
				}

				// Add instance flags to any object list flags added to actions or other objects.
				// defined by Add*Flag*FromObjectListAction* like functions
				for _, flag_list := range l.flags_list {
					// Do not recreate if already exist.
					if _, found := flag_list.params[flag_name]; found {
						continue
					}

					switch {
					case flag_list.action != nil:
						f := new(ForjFlag)
						f.list = l
						f.instance_name = instance_name
						f.field_name = field_name
						p := ForjParam(f)
						p.set_cmd(flag_list.action.cmd, field.value_type, flag_name, field.help+" for "+instance_name, nil)
						flag_list.action.params[flag_name] = p
						flag_list.params[flag_name] = p
					case flag_list.objectAction != nil:
						f := new(ForjFlag)
						f.list = l
						f.instance_name = instance_name
						f.field_name = field_name
						p := ForjParam(f)
						p.set_cmd(flag_list.objectAction.cmd, field.value_type, flag_name, field.help+" for "+instance_name, nil)
						flag_list.objectAction.params[flag_name] = p
						flag_list.params[flag_name] = p
					}
				}
			}
		}
	}
}

// loadObjectData is executed at final Parse task. ParseContext time is over. So kingpin has delivered data.
// It loads Object data from any other object/instance flags
// and update the cli object data fields list
func (c *ForjCli) loadObjectData() error {
	var params map[string]ForjParam
	switch {
	case c.cli_context.list != nil: // <app> <action> <object>s
		l := c.cli_context.list
		params = l.actions[c.cli_context.action.name].params
	case c.cli_context.object != nil: // <app> <action> <object>
		o := c.cli_context.object
		params = o.actions[c.cli_context.action.name].params
	case c.cli_context.action != nil: // <app> <action>
		a := c.cli_context.action
		params = a.params
	}
	for _, param := range params {
		if p, ok := param.(forjParamObject); ok {
			if err := p.UpdateObject(); err != nil {
				return fmt.Errorf("Unable to load Object data. %s", err)
			}
		}
	}
	return nil
}

func (c *ForjCli) identifyObjects(cmd clier.CmdClauser) {
	c.cli_context.action = nil
	c.cli_context.object = nil
	c.cli_context.list = nil
	// Identify in Actions, in Objects, then in ObjectList
	for _, action := range c.actions {
		if action.cmd.IsEqualTo(cmd) {
			// ex: forjj =>create<=
			c.cli_context.action = action
			return
		}
	}

	for _, object := range c.objects {
		for _, action := range object.actions {
			if action.cmd.IsEqualTo(cmd) {
				// ex: forjj add =>repo<=
				c.cli_context.object = object
				c.cli_context.action = action.action
				return
			}
		}
	}

	for _, list := range c.list {
		for _, action := range list.actions {
			if action.cmd.IsEqualTo(cmd) {
				// ex: forjj add =>repos<=
				c.cli_context.action = action.action
				c.cli_context.object = list.obj
				c.cli_context.list = list
			}
		}
	}
}

// check List flag and start creating object instance.
func (c *ForjCli) getContextParam(object, key, param_name string) ForjParam {

	// check if the ObjectList is found.
	// Ex: forjj create repos <list>
	if l := c.cli_context.list; l != nil {
		gotrace.Trace("Checking if '%s/%s/%s' is found in the current object list.", object, key, param_name)

		if v := l.search_object_param(c.cli_context.action.name, object, key, param_name); v != nil {
			return v
		}
		return nil
	}

	if o := c.cli_context.object; o != nil {
		gotrace.Trace("Checking if '%s/%s/%s' is found in the current object.", object, key, param_name)

		if v := o.search_object_param(c.cli_context.action.name, object, key, param_name); v != nil {
			return v
		}
		return nil
	}

	if a := c.cli_context.action; a != nil {
		gotrace.Trace("Checking if '%s/%s/%s' is found in the current action.", object, key, param_name)

		if v := a.search_object_param(object, key, param_name); v != nil {
			return v
		}
		return nil
	}
	return nil
}

// LoadValuesFrom load most of flags/arguments found in the cli context in values, like kingpin.execute do.
func (c *ForjCli) LoadValuesFrom(context clier.ParseContexter) {
	c.loadListValuesFrom(context)
	c.loadObjectValuesFrom(context)
	c.loadActionValuesFrom(context)
	c.loadAppValuesFrom(context)
}

func (c *ForjCli) loadListValuesFrom(context clier.ParseContexter) {
	if c.cli_context.list == nil {
		return
	}
	for _, action := range c.cli_context.list.actions {
		if action.action == c.cli_context.action {
			for _, param := range action.params {
				param.loadFrom(context)
			}
		}
	}
}

func (c *ForjCli) loadObjectValuesFrom(context clier.ParseContexter) {
	if c.cli_context.object == nil {
		return
	}
	for _, action := range c.cli_context.object.actions {
		if action.action == c.cli_context.action {
			for _, param := range action.params {
				param.loadFrom(context)
			}
		}
	}
}

func (c *ForjCli) loadActionValuesFrom(context clier.ParseContexter) {
	if c.cli_context.action == nil {
		return
	}
	for _, param := range c.cli_context.action.params {
		param.loadFrom(context)
	}
}

func (c *ForjCli) loadAppValuesFrom(context clier.ParseContexter) {
	for _, flag := range c.flags {
		flag.loadFrom(context)
	}
}

// GetStringValueAddr : Provide the Address to the flag/argument value if is a *string.
// It returns nil if the flag/arg was not found or is not a string.
func (c *ForjCli) GetStringValueAddr(name string) *string {
	if v, found := c.flags[name]; found {
		if is_string(v.flagv) {
			return v.flagv.(*string)
		}
		return nil
	}
	return nil
}
